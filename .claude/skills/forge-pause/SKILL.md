---
name: forge-pause
description: Pause the forge - stop the orchestrator so no spec or story is picked up and no new agent run starts, optionally killing the agents running now. Use for "pause the forge", "stop the agents", "stop spending", "hold the workflow". Undo with forge-resume.
---

# Forge pause

Work from the forge root. Gitea, the runner and staging keep running; only the orchestrator stops.

1. See what runs now: `curl -s localhost:8000/health` (the `running` list) and `docker ps --filter label=forge.agent`.
2. If agents are running, ask the user: let them finish (they keep spending until they exit, and their results are only handled after resume) or kill them now (uncommitted work is lost; those stories redo the run after resume).
3. `docker compose stop orchestrator`.
4. To kill them: `docker rm -f $(docker ps -q --filter label=forge.agent)`.
5. Check: `docker compose ps orchestrator` shows it exited, `docker ps --filter label=forge.agent` matches the choice in step 2.

Report what was running and what you stopped. CI jobs already queued in Gitea still run; their results wait for resume.

To hold back only one story or spec instead, remove its `us-todo` or `spec` label: nothing picks it up until the label is back.
