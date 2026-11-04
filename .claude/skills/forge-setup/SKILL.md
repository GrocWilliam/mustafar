---
name: forge-setup
description: Install, start, update or repair the forge itself (Gitea, runner, agent image, orchestrator) - first-time setup, restart after a reboot, redeploy after changing the orchestrator, agents or prompts, roll a CI template change out to projects. Use for "set up the forge", "start the forge", "the forge is down", "rebuild the orchestrator", "apply my changes".
---

# Forge setup

The steps of record are in `README.md`; this skill makes them safe to rerun. Work from the forge root. Check each step's state before acting, and skip what is already done.

Secrets: never print `.env` values, tokens, the admin password or the private key. Write tokens into `.env` with `sed -i "s|^KEY=.*|KEY=$value|" .env` straight from the command that produced them, without echoing them.

## First-time setup

1. `.env` exists (else `cp .env.example .env`). `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` must be set: only the user has it, so ask them to fill it in themselves (`! $EDITOR .env`).
2. `secrets/deploy_key` exists, else `ssh-keygen -t ed25519 -N '' -C forge-agents -f secrets/deploy_key`.
3. Optional: a `127.0.0.1 gitea` hosts entry only makes `http://gitea:3000` links open in the browser. The host tools use `http://localhost:3000`, so skip it when the user can't edit the hosts file.
4. `docker compose up -d gitea`, then wait until `curl -sf http://localhost:3000/api/healthz` answers.
5. Admin `forge`: if `secrets/gitea-admin-password` is missing, create it and the user as in README step 2.
6. Tokens: if `GITEA_TOKEN` is empty, generate it (README step 3, scopes `write:repository,write:issue`) into `.env`; same for `GITEA_RUNNER_REGISTRATION_TOKEN`.
7. `docker compose up -d runner && docker compose build agent && docker compose up --build -d orchestrator` (the orchestrator build runs `go vet` and the tests).
8. Verify: `curl -s localhost:8000/health` answers, and `docker compose logs --tail 30 orchestrator` shows no `poll failed` / `protecting` error. A `protecting main: ... 403` error means the token can't create branch protection: regenerate it with more rights.

Then offer the `forge-spec` skill to create a first project.

## After a reboot or a crash

`docker compose up -d gitea runner orchestrator`, then step 8. Stories that were in progress go back to the queue (`♻️` comment) and resume their branch: that is expected.

## After changing the forge's code

- `orchestrator/*.go`: `cd orchestrator && go vet ./... && go test ./...`, then `docker compose up --build -d orchestrator`.
- `ci/Dockerfile`: `docker compose build agent` (rebuilds `forge-ci` too), then `docker compose restart runner` once no CI job runs.
- `agents/agent.sh`, `agents/prompts/*`, `agents/Dockerfile`: `docker compose build agent` (the next agent run uses it; no restart needed).
- `.env`: `docker compose up -d orchestrator` (recreates it).
- `templates/ci.yml`: every project keeps its own copy in `.gitea/workflows/ci.yml`, and `main` refuses pushes. For each project (`./forge-api.sh GET "/repos/search?topic=true&q=forge&limit=50"`), create a branch with the new file through the contents API and open a pull request, then ask the user to merge them in the web UI. Never disable the branch protection to push it.

Restarting the orchestrator interrupts running stories: check `curl -s localhost:8000/health` first, and tell the user what will restart.
