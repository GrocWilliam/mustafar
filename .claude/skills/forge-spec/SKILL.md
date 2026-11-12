---
name: forge-spec
description: Write a spec the forge can build and open it, creating the project (Gitea repo) first when needed. Use for "build X with the forge", "new forge project", "add a spec", "add a feature to <project>", "open a spec".
---

# Forge spec

A spec is a Gitea issue labelled `spec` in a project (a repo with the `forge` topic). Opening one starts paid, autonomous agent runs: planner, then per story tester, dev (up to 3 attempts), reviewer, merge into the spec's branch, then every acceptance test on the whole spec, an integrator agent if needed, and the merge to `main` and staging. So **show the final spec and get the user's OK before opening it.**

Work from the forge root. Never print the admin password or `.env` values.

API calls go through `./forge-api.sh METHOD PATH [JSON | @file]` (prints the body, fails on an HTTP error).

## 1. Settle what to build

A new project with no settled product, stack or design: use `forge-prd` first; it writes the project's reference docs, then comes back here for each spec. A project with `docs/prd.md` or `docs/architecture.md`: read them and write the spec within them.

Runs are paid and nobody answers the agents, so settle decisions here. For an existing project, read its code first and never ask what it already answers. Skip what the request already settles.

**Scope.** When the request bundles capabilities that have their own users or data and can be tested separately, propose a capability map and get it agreed before writing: a table of modules (kebab-case id, responsibility, depends on), dependencies in one direction with no cycles, and the build order. Each module becomes its own spec, opened in build order and linked with `Depends on #N`.

**Interview.** Decisions form a tree: each one opens those that depend on it. Work in rounds. Each round asks, numbered, every open question whose prerequisites are settled, with your recommended answer, worded so that "yes" accepts it. Wait for the answers, then start the next round, until nothing that changes what gets built is open (users, core features, stack, business rules, data to keep). Decide the rest yourself and state it in the spec.

## 2. Write the spec

Save it to a file in your scratchpad: first line `# Title`, then the body. Model: `examples/url-shortener/spec.md`. Nobody answers the agents' questions, so the spec must settle everything that matters:

- **Concrete, testable behavior**: inputs and exact outputs, commands and their output, HTTP requests with status codes and bodies, error cases. No "fast", "nice", "robust".
- **Interfaces by name**: modules, functions, CLI commands, endpoints, file formats, how to start it (`python3 -m app`, `node server.js`...).
- **Stack**: language and whether dependencies are allowed. CI and agents run Debian bookworm with Node 26, Python 3, make, git and Chromium (`$CHROMIUM_PATH`, for browser tests); anything else must be installable by `make test`.
- **A service** must listen on port 8080 (or `$PORT`) and answer `GET /health` with 200: the planner then adds a `Dockerfile` and every spec reaching `main` is deployed to staging. Tests never use Docker.
- **Size**: at most 20 stories of one ~30-turn dev session each. Split a bigger product into successive specs, using the capability map.
- **Existing project**: read its code first (`git clone http://localhost:3000/$repo.git` into the scratchpad) and describe only the change, in the terms the code already uses.
- **Boundaries**: an `Always` and a `Never` list for what the build must preserve or stay away from (data to keep, URLs that must not change, dependencies not allowed). No "ask first": nobody answers, so make each one an always or a never.

## 3. Open it

- **New project** (repo name: lowercase, `[a-z0-9-]`): `./new-project.sh NAME /path/to/spec.md`. It creates the repo, deploy key, CI workflow and `forge` topic, waits for the orchestrator to create the labels, and opens the spec. Check first that the orchestrator runs (`curl -s localhost:8000/health`).
- **After another spec**: end the body with a line `Depends on #N`; the orchestrator plans it only once #N is closed.
- **Existing project** `owner/repo`: get the `spec` label id from `./forge-api.sh GET "/repos/$repo/labels"`, then
  `./forge-api.sh POST "/repos/$repo/issues" "{\"title\": ..., \"body\": ..., \"labels\": [ID]}"`, building the JSON with `python3 -c 'import json,sys; ...'` so quotes and newlines survive.

## 4. Hand over

Give the spec link. Tell the user: the plan appears as a `📋 Plan` comment within a few minutes, each story shows its agents' progress as comments, staging runs on `localhost:<9000 + repo id>` (the `🚀` comment gives it), and the `forge-status` skill reports progress and cost.
