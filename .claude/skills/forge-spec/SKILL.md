---
name: forge-spec
description: Write a spec the forge can build and open it, creating the project (Gitea repo) first when needed. Use for "build X with the forge", "new forge project", "add a spec", "add a feature to <project>", "open a spec".
---

# Forge spec

A spec is a Gitea issue labelled `spec` in a project (a repo with the `forge` topic). Opening one starts paid, autonomous agent runs: planner, then per story tester, dev (up to 3 attempts), reviewer, merge, staging deploy. So **show the final spec and get the user's OK before opening it.**

Work from the forge root. Never print the admin password or `.env` values.

```bash
api() { curl -sf -X "$1" "http://gitea:3000/api/v1$2" -u "forge:$(cat secrets/gitea-admin-password)" -H 'Content-Type: application/json' ${3:+-d "$3"}; }
```

## 1. Write the spec

Save it to a file in your scratchpad: first line `# Title`, then the body. Model: `examples/url-shortener/spec.md`. Nobody answers the agents' questions, so the spec must settle everything that matters:

- **Concrete, testable behavior**: inputs and exact outputs, commands and their output, HTTP requests with status codes and bodies, error cases. No "fast", "nice", "robust".
- **Interfaces by name**: modules, functions, CLI commands, endpoints, file formats, how to start it (`python3 -m app`, `node server.js`...).
- **Stack**: language and whether dependencies are allowed. CI and agents run Debian bookworm with Node 26, Python 3, make, git and Chromium (`$CHROMIUM_PATH`, for browser tests); anything else must be installable by `make test`.
- **A service** must listen on port 8080 (or `$PORT`) and answer `GET /health` with 200: the planner then adds a `Dockerfile` and every merge is deployed to staging. Tests never use Docker.
- **Size**: at most 20 stories of one ~30-turn dev session each. Split a bigger product into successive specs.
- **Existing project**: read its code first (`git clone http://gitea:3000/$repo.git` into the scratchpad) and describe only the change, in the terms the code already uses.

Ask the user only about real gaps (stack, a business rule); state the other choices in the spec.

## 2. Open it

- **New project** (repo name: lowercase, `[a-z0-9-]`): `./new-project.sh NAME /path/to/spec.md`. It creates the repo, deploy key, CI workflow and `forge` topic, waits for the orchestrator to create the labels, and opens the spec. Check first that the orchestrator runs (`curl -s localhost:8000/health`).
- **After another spec**: end the body with a line `Depends on #N`; the orchestrator plans it only once #N is closed.
- **Existing project** `owner/repo`: get the `spec` label id from `api GET "/repos/$repo/labels"`, then
  `api POST "/repos/$repo/issues" "{\"title\": ..., \"body\": ..., \"labels\": [ID]}"`, building the JSON with `python3 -c 'import json,sys; ...'` so quotes and newlines survive.

## 3. Hand over

Give the spec link. Tell the user: the plan appears as a `📋 Plan` comment within a few minutes, each story shows its agents' progress as comments, staging runs on `localhost:<9000 + repo id>` (the `🚀` comment gives it), and the `forge-status` skill reports progress and cost.
