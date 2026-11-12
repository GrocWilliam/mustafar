# Forge

A local, autonomous software forge. You write a spec as a Gitea issue; headless Claude Code agents plan it into stories, write the acceptance tests, implement, and review. The orchestrator merges and deploys to staging on its own, following fixed rules: green CI + reviewer approval → merge; failed health check → revert.

Everything runs on one machine with Docker Compose, and every step is logged as a comment on the Gitea issue. One forge serves many projects. The design is in [`AGENT.md`](AGENT.md).

## How it works

```
spec issue ──planner──▶ us-todo stories (with dependencies)
                          │
                          ▼
              tester ─▶ dev ─▶ CI ─▶ reviewer ─▶ squash-merge into spec/<n> ─▶ (last story) main ─▶ staging deploy
                         ▲      │        │                          │
                         └──────┴────────┘                   /health fails?
                       retry, up to 3 attempts               → revert + us-blocked
```

| Component | Role |
|---|---|
| `gitea` | Forge, work queue and source of truth. Web on `:3000`, SSH on `:2222`. |
| `runner` | Gitea Actions runner: CI only (`make test` + `tests/acceptance/*/run.sh`). No LLM. |
| `orchestrator` | Go service on `:8000`, which also serves the forge UI. The only thing that changes labels; dispatches agents, merges, deploys, reverts. |
| `agent` | One-shot Claude Code container, one role per run: `planner`, `tester`, `dev`, `reviewer`, `retester` (revises tests the dev reports as conflicting), `integrator` (fixes what stops a finished spec reaching `main`). |
| staging | One container per project, `forge-staging-<owner>-<repo>`, on `localhost:<9000 + repo id>`. |

## Setup

Requires Docker with Compose, `openssl`, `ssh-keygen`, `python3` (for `new-project.sh` and `bench.sh`), and a Claude API key or subscription token.

```bash
# 1. Secrets (the host reaches Gitea on localhost:3000, containers on gitea:3000)
cp .env.example .env                                  # fill in ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN
mkdir -p secrets && ssh-keygen -t ed25519 -N '' -C forge-agents -f secrets/deploy_key
echo '127.0.0.1 gitea' | sudo tee -a /etc/hosts    # optional: only for gitea:3000 links in the browser

# 2. Gitea (no web wizard) and its admin user `forge`; the password lands in secrets/
docker compose up -d gitea
(umask 077; openssl rand -base64 18 > secrets/gitea-admin-password)
docker compose exec -T -u git gitea gitea admin user create --admin --username forge \
  --email forge@forge.local --password "$(cat secrets/gitea-admin-password)" --must-change-password=false

# 3. Tokens: put the first in .env as GITEA_TOKEN, the second as GITEA_RUNNER_REGISTRATION_TOKEN
docker compose exec -T -u git gitea gitea admin user generate-access-token \
  -u forge -t orchestrator --scopes write:repository,write:issue,write:user --raw # write:user: creating projects from the UI
docker compose exec -T -u git gitea gitea actions generate-runner-token

# 4. Runner, agent image, orchestrator (tests run during its build; it creates the labels on startup)
docker compose up -d runner
docker compose build agent
docker compose up --build -d orchestrator

# 5. A first project: Gitea repo, deploy key, CI workflow, `forge` topic
./new-project.sh sandbox
```

Check it is up: `curl localhost:8000/health`.

## Usage

### Projects

Every Gitea repo with the `forge` topic is a project. Add one with `./new-project.sh NAME [SPEC_FILE]` or from the forge UI (**＋ Project**: an empty repo, an import from a clone URL, or an existing Gitea repo), no restart needed; with a spec file the script also opens it as a `spec` issue.

To keep a copy on GitHub, GitLab, Azure DevOps or any HTTPS git host, create an empty repo there, then add it in the forge UI (project tab › Remote mirrors) or run `MIRROR_TOKEN=<token with write access> ./mirror.sh NAME https://host/owner/repo.git` (for GitLab add `MIRROR_USER=oauth2`). Gitea then pushes every branch and tag on each commit. It is one-way: issues and specs stay in Gitea, and anything pushed to the remote directly is overwritten.

Worked example with every step shown: [`examples/url-shortener/WALKTHROUGH.md`](examples/url-shortener/WALKTHROUGH.md) (`./new-project.sh shortener examples/url-shortener/spec.md`).

### Build a feature: `spec` label

Open an issue describing the feature with the `spec` label. The planner comments a plan and creates the stories, which run in dependency order, up to `MAX_AGENTS` at once; independent stories run in parallel. The spec closes when all its stories are done.

### Build one story: `us-todo` label

Open an issue with explicit acceptance criteria and the `us-todo` label (e.g. in `forge/sandbox`). You should see:

1. The issue moves to `us-in-progress`; `tester` then `dev` comments show progress and a summary.
2. A PR from `agent/us-<n>` opens (tester commit + dev commit) and the issue moves to `us-review`.
3. The `ci` workflow runs (repo › Actions).
4. A `reviewer` comment ends with `VERDICT: approve`; the PR is squash-merged and the issue closed.

Rejections show up as `🔁 Attempt n/3` comments with the feedback given to the dev. After 3 attempts the issue moves to `us-blocked`. If its target branch moved under a finished story, a `🔄` comment shows the orchestrator merging it into the branch and rerunning CI first.

Stories merge into their spec's own branch, `spec/<n>`, so specs built in parallel do not collide. Each story runs every acceptance test, so an earlier test it breaks shows up on that story: its tester updates the test when the story changes that behavior on purpose, and the dev fixes the code otherwise. Once its last story is closed, every acceptance test runs again on the whole spec. If that fails or conflicts with `main`, an `integrator` agent fixes it, then the orchestrator merges the spec into `main` through one pull request.

### Staging

Once a project has a `Dockerfile` (the planner adds one when a spec describes a service), every spec reaching `main` is deployed to `http://localhost:<9000 + repo id>`; the 🚀 comment gives the URL. If `GET /health` does not answer 200 within a minute, the merge is reverted on `main`, the previous version redeployed, and the spec set to `us-blocked`.

### Retrying blocked work

| Blocked | Retry by setting |
|---|---|
| Story (`us-blocked`) | `us-todo` (resumes its branch if the dev had started; delete `agent/us-<n>` to start over from the tester) |
| Spec, planning failed | `spec` |
| Spec, over budget | `spec-active`, after raising its budget in the forge UI or `MAX_COST_PER_SPEC` |

## Configuration (`.env`)

| Variable | Default | Meaning |
|---|---|---|
| `ANTHROPIC_API_KEY` / `CLAUDE_CODE_OAUTH_TOKEN` | — | Claude credentials, set one (`claude setup-token` gives the second). |
| `GITEA_TOKEN` | — | Orchestrator API token, never given to agents. |
| `GITEA_RUNNER_REGISTRATION_TOKEN` | — | act_runner registration. |
| `POLL_SECONDS` | `30` | How often the orchestrator polls Gitea. |
| `MAX_AGENTS` | `2` in `.env.example` | Specs and stories worked on at once, across all projects. Keep `runner/config.yaml` capacity at least 4 times as high: each commit runs 4 CI jobs. |
| `MAX_COST_PER_SPEC` | `20` | USD; a spec's remaining stories are blocked once its agents cost more. |
| `MODEL_LOW` / `MODEL_MEDIUM` / `MODEL_HIGH` | `haiku` / `sonnet` / `opus` | Claude model per story complexity, which the planner rates in a `Complexity:` line of each story (edit it to change a story's model). Unrated stories use the medium one; the planner runs on the high one. |
| `PUBLISH_IP` | `127.0.0.1` | Where the forge UI (`:8000`) and staging apps (`9000 + repo id`) listen. `0.0.0.0` opens them to the LAN (a phone at home); the UI has no login, so trusted networks only. |

`POLL_SECONDS`, `MAX_AGENTS`, `MAX_COST_PER_SPEC` and the `MODEL_*` are defaults: changes made in the forge UI (**⚙ Settings**) apply at once, are saved in the `orchestrator-data` volume and win over `.env`. Delete `/data/settings.json` there to go back to `.env`.
| `PROTECTED_PATHS` | `.gitea/` | Comma-separated path prefixes a PR must not touch; such a PR is blocked, never merged. |

## Operations

- **Logs:** `docker compose logs -f orchestrator runner`
- **Forge UI:** http://localhost:8000 shows projects, specs and stories with their pipeline step and live agent activity, and one tab per project, and has buttons to retry, set a spec's budget, open a spec, add a project, manage push mirrors and change the settings (localhost only).
- **Status:** `curl localhost:8000/health` lists issues being worked on, as `owner/repo#n`.
- **Benchmark:** `./bench.sh [SPEC_FILE]` builds a spec (default `examples/url-shortener/spec.md`) in a fresh `bench-*` project, waits until it is done or blocked, and appends spend, agent runs, retries and duration to `bench-results.tsv`, tagged with the forge commit. Run it before and after changing prompts or skills. It spends real money, like any spec.
- **CI template change:** the orchestrator protects `main` against every push, so update each existing project's `.gitea/workflows/ci.yml` from `templates/ci.yml` through a PR merged in the web UI.

## Claude Code skills

Available in any Claude Code session opened in this directory (`.claude/skills/`):

- `forge-setup`: install, start, rebuild after changes
- `forge-prd`: start a new project from an idea: a guided interview that writes its PRD, architecture and design docs, then opens its first spec
- `forge-spec`: write a spec and open it, creating the project if needed
- `forge-status`: projects, running work, blocked issues, spend
- `forge-unblock`: diagnose and retry a blocked story or spec
- `forge-pause` / `forge-resume`: stop the orchestrator (and optionally running agents), then start it again
