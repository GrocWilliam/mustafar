# Forge — setup (M6)

A `spec` issue is planned into `us-todo` stories with dependencies. Each story gets acceptance tests (tester), an implementation (dev, up to 3 attempts), CI and a review; once approved, the PR is squash-merged and the issue closed.
See `AGENT.md` for the design.

```bash
# 1. Secrets, and one URL for Gitea everywhere (host, containers, CI jobs)
cp .env.example .env                                  # fill in ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN
mkdir -p secrets && ssh-keygen -t ed25519 -N '' -C forge-agents -f secrets/deploy_key
echo '127.0.0.1 gitea' | sudo tee -a /etc/hosts

# 2. Gitea (no web wizard) and its admin user `forge`; the password lands in secrets/
docker compose up -d gitea
(umask 077; openssl rand -base64 18 > secrets/gitea-admin-password)
docker compose exec -T -u git gitea gitea admin user create --admin --username forge \
  --email forge@forge.local --password "$(cat secrets/gitea-admin-password)" --must-change-password=false

# 3. Tokens -> .env: orchestrator API token (GITEA_TOKEN) and runner registration token
docker compose exec -T -u git gitea gitea admin user generate-access-token \
  -u forge -t orchestrator --scopes write:repository,write:issue --raw
docker compose exec -T -u git gitea gitea actions generate-runner-token

# 4. Runner, agent image, orchestrator (tests run during its build; it creates the labels on startup)
docker compose up -d runner
docker compose build agent
docker compose up --build -d orchestrator

# 5. A project: Gitea repo, deploy key, CI workflow, `forge` topic; with a spec file, also opens it as a `spec` issue.
./new-project.sh sandbox
```

One forge serves every repo tagged with the `forge` topic: `./new-project.sh NAME [SPEC_FILE]` adds one, no restart needed.
A worked example, every step shown: [`examples/url-shortener/WALKTHROUGH.md`](examples/url-shortener/WALKTHROUGH.md) (`./new-project.sh shortener examples/url-shortener/spec.md`).

Then create an issue in a project (e.g. `forge/sandbox`) with explicit acceptance criteria and the `us-todo` label, and check:

1. it moves to `us-in-progress`; a `tester` comment, then a `dev` comment show progress and a summary;
2. a PR from `agent/us-<n>` opens (tester commit + dev commit), the issue moves to `us-review`;
3. the `ci` workflow runs `make test` and `tests/acceptance/*/run.sh` (repo › Actions);
4. a `reviewer` comment ends with `VERDICT: approve`, the PR is squash-merged and the issue closed.
   Rejections show up as `🔁 Attempt n/3` comments with the feedback given to the dev; after 3 the issue moves to `us-blocked`.

To build a whole feature, open an issue describing it with the `spec` label instead: the planner comments a plan and creates the stories, which run in dependency order, up to `MAX_AGENTS` at once (stories that do not depend on each other run in parallel); the spec closes when they are all done. When `main` moved under a finished story, a `🔄` comment shows the orchestrator merging it into the branch and rerunning CI before the merge.

A blocked US is retried by setting `us-todo` again: it starts over from the tester. A blocked spec is retried by setting `spec` again (planning failed) or `spec-active` (over budget: raise `MAX_COST_PER_SPEC` first).
Once the target repo has a `Dockerfile` (the planner adds one when a spec describes a service), every merge is deployed to that project's staging at http://localhost:<9000 + repo id> (container `forge-staging-<owner>-<repo>`; the 🚀 comment gives the URL). If `GET /health` does not answer 200 within a minute, the merge is reverted on `main`, the previous version redeployed, and the US reopened as `us-blocked`.
Existing target repos: update `.gitea/workflows/ci.yml` from `templates/ci.yml` through a pull request merged in the web UI (the orchestrator protects `main` against every push).
Logs: `docker compose logs -f orchestrator runner` · status: `curl localhost:8000/health` (issues being worked on, as `owner/repo#n`).

Claude Code skills (in `.claude/skills/`, available in any Claude Code session opened here): `forge-setup` (install, start, rebuild after changes), `forge-spec` (write a spec and open it, creating the project if needed), `forge-status` (projects, running work, blocked issues, spend), `forge-unblock` (diagnose and retry a blocked story or spec).
