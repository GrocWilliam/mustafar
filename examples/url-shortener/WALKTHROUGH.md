# Walkthrough: from spec to staging

This example builds the service described in [`spec.md`](spec.md), and shows each step of the forge on it.
Issue numbers, comments and code below are what a typical run looks like: the agents write their own, so yours differ in details.

## 0. Prerequisites (once per forge)

The README setup steps 1–3 and 5: Gitea, the `forge` admin, `.env` with the Claude credentials and `GITEA_TOKEN`, the runner, the agent image and the orchestrator running.

## 1. Create the project and open the spec

```bash
./new-project.sh shortener examples/url-shortener/spec.md
# created http://gitea:3000/forge/shortener
# spec opened: http://gitea:3000/forge/shortener/issues/1
```

The script creates the repo, adds the agents' deploy key and the CI workflow, then tags the repo with the `forge` topic.
On its next poll (30 s), the orchestrator logs `forge/shortener: new project`, creates the labels, protects `main` against pushes, and the script opens issue #1 with the `spec` label.

## 2. Planning (`planner`)

On #1, a comment `🤖 Agent planner working on #1` updates as it reads the repository, then becomes its summary (steps, turns, cost). Its final message ends with a JSON plan, which the orchestrator validates, then:

```
📋 Plan: 4 stories, built in this order.

- #2 Set up the Python project, GET /health and the Dockerfile
- #3 Shorten a URL with POST /shorten (after #2)
- #4 Redirect GET /<code> to the original URL (after #3)
- #5 Count redirects in GET /stats/<code> (after #4)
```

#1 moves `spec` → `spec-active`. Issues #2–#5 are created, each ending with `Part of #1` and its `Depends on #n` lines, then labelled `us-todo`.
Here every story depends on the previous one, so they run one after another; independent stories would run in parallel, up to `MAX_AGENTS`.

## 3. Acceptance tests (`tester`) — story #3

#3 moves `us-todo` → `us-in-progress`. The tester starts `agent/us-3` from `main` and commits only under `tests/acceptance/`:

```
tests/acceptance/us-3/
├── run.sh            # starts `python3 -m shortener` on a free port, runs the tests, stops it
└── test_shorten.py   # one test per criterion: 201 + 6-char code, same URL same code, 400 cases
```

It checked that they fail (the endpoint does not exist yet). The orchestrator then checks the tester changed nothing outside `tests/acceptance/` and wrote something in `tests/acceptance/us-3/`.

## 4. Implementation (`dev`), attempt 1

The dev checks out the branch, writes `shortener/` and its unit tests, runs `make test` and `bash tests/acceptance/us-3/run.sh`, and commits `feat: add POST /shorten`. The orchestrator then:

- opens the pull request `agent/us-3` → `main` with `Closes #3` (`🔀 Pull request opened: …`), and moves #3 to `us-review`;
- checks the dev left every acceptance test as the tester committed it, and touched nothing under `.gitea/`.

## 5. CI

The `ci` workflow (repo › Actions) runs the `test` job (`make test`), then the `acceptance` job from a fresh checkout (every `tests/acceptance/*/run.sh`, so #2's tests too: they are the regression suite). The orchestrator waits for the commit status.

## 6. Review (`reviewer`) and a retry

The reviewer reads `git diff origin/main...HEAD` against the story. Say it finds a gap:

```
- shortener/app.py: a URL like `ftp://x` gets 400, but `HTTPS://EXAMPLE.COM` also gets 400; the story only says
  "starts with http:// or https://", so the scheme check must be case-insensitive or the criterion stated as is…

VERDICT: changes
```

The orchestrator posts `🔁 Attempt 2/3. Feedback for the dev agent:` with that text, reruns the dev with it, then the checks, CI and the reviewer again. A red CI or a modified frozen test gives the same kind of feedback. After 3 rejected attempts, the story goes to `us-blocked`.

## 7. Merge

On `VERDICT: approve` with green CI, the orchestrator takes the project's merge lock. If `main` moved meanwhile (another story merged), it first merges `main` into the branch (`🔄 main moved by 1 commit(s)…`) and waits for CI again. Then it squash-merges the pull request, pinned to the commit that was checked: `Shorten a URL with POST /shorten (#3)`.

## 8. Deployment to staging

`main` has the `Dockerfile` from #2, so the orchestrator builds it as `forge-app/forge/shortener:<sha>` and replaces the container `forge-staging-forge-shortener`, published on `localhost:<9000 + repo id>`:

```
✅ CI green and review approved, pull request merged: http://gitea:3000/forge/shortener/pulls/2

🚀 Deployed to staging (http://localhost:9002), `/health` answers 200.
```

#3 is closed, which unblocks #4. Had `/health` not answered 200 within a minute, the orchestrator would have merged a `Revert …` pull request, redeployed the previous version, and reopened #3 as `us-blocked` with the reason.

## 9. Done

Once #5 is closed, the spec gets `✅ All 4 stories are done. Total agent cost: $x.xx.` and is closed. Try the result:

```bash
curl -s -X POST localhost:9002/shorten -d '{"url": "https://example.com/a"}'
# {"code": "aZ3k9Q", "short_url": "http://localhost:8080/aZ3k9Q"}
curl -si localhost:9002/aZ3k9Q | grep -i location     # Location: https://example.com/a
curl -s localhost:9002/stats/aZ3k9Q                    # {"url": "https://example.com/a", "hits": 1}
git clone http://gitea:3000/forge/shortener.git        # the code, its tests, its history
```

(`short_url` says `localhost:8080`, the port inside the container, as the spec asked.)

## When it stops

A `us-blocked` issue and its `⛔ Blocked:` comment say why. Fix the story (or the spec) and set `us-todo` again on the story, or `spec` again on a spec whose planning failed. Over budget: raise `MAX_COST_PER_SPEC`, restart the orchestrator, then set `spec-active` on the spec and `us-todo` on its blocked stories.
