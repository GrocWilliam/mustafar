---
name: forge-resume
description: Resume a forge paused with forge-pause - start the orchestrator again so queued specs and stories run. Use for "resume the forge", "unpause", "start the agents again". For a first install, a reboot or a down Gitea, use forge-setup.
---

# Forge resume

Work from the forge root.

1. `docker compose ps`: if `gitea` or `runner` is not running, this is not a pause; use forge-setup instead.
2. `docker compose start orchestrator`.
3. Wait until `curl -sf localhost:8000/health` answers. On startup it removes leftover agent containers and puts the stories that were in progress back in the queue (`♻️` comment), resuming their branch: expected.
4. `docker compose logs --tail 20 orchestrator` to confirm it started without errors.

Report that the forge runs again and what it picked up; the forge UI (http://localhost:8000) and forge-status show the progress.
