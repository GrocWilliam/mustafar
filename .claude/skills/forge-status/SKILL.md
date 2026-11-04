---
name: forge-status
description: Report what the forge is doing - projects, specs and stories per lifecycle label, what agents run now, blocked issues and why, spend per spec, service health. Use for "forge status", "what is the forge doing", "is my spec done", "how much did it cost", "is the forge up". Read-only.
---

# Forge status

Read-only: change nothing, start nothing.

Run everything from the forge root (the directory holding `docker-compose.yml`), with this helper. Never print the admin password or `.env` values. Use `python3` to parse JSON (`jq` may be missing).

```bash
api() { curl -sf -X "$1" "http://gitea:3000/api/v1$2" -u "forge:$(cat secrets/gitea-admin-password)" -H 'Content-Type: application/json' ${3:+-d "$3"}; }
```

## Collect

1. Services: `docker compose ps` (gitea, runner, orchestrator) and `curl -s localhost:8000/health`, whose `running` lists the issues agents work on now, as `owner/repo#n`. No answer → the orchestrator is down: show `docker compose logs --tail 30 orchestrator`.
2. Projects: `api GET "/repos/search?topic=true&q=forge&limit=50"` → `.data[].full_name`.
3. Per project, open issues per label (`spec`, `spec-active`, `us-todo`, `us-in-progress`, `us-review`, `us-blocked`):
   `api GET "/repos/$repo/issues?state=open&type=issues&limit=50&labels=$label"`.
4. Per `us-blocked` issue: its last comment starting with `⛔ Blocked:` (`api GET "/repos/$repo/issues/$n/comments"`) is the reason.
5. Per `spec-active` spec, if the user asks about progress or cost:
   - its stories are the numbers in the `<!-- forge:stories 2,3,4 -->` marker of its last plan comment; count how many are closed;
   - spend = sum of every `<!-- forge:cost=X -->` in the comments of the spec and of its stories. Compare with `MAX_COST_PER_SPEC` (`grep -E '^MAX_COST_PER_SPEC=' .env`, default 20).
6. Recent errors: `docker compose logs --since 30m orchestrator | grep -iE 'fail|error|blocked' | tail -20`.
7. Drift, per project (always; it is three calls):
   - CI workflow: `api GET "/repos/$repo/contents/.gitea/workflows/ci.yml?ref=main"`; base64-decode `.content` and
     compare it with `templates/ci.yml`. Different → the project missed a CI template rollout (`forge-setup` rolls it
     out). Missing → its CI runs nothing, and every story will fail.
   - Branch protection: `api GET "/repos/$repo/branch_protections"` must hold a rule named `main` with
     `enable_push: false`. Without it agents could push to `main`: the orchestrator restores it on its next start.
   - Memory: `api GET "/repos/$repo/contents/CLAUDE.md?ref=main"`; still holding `_not written yet` while the project
     has closed stories (`api GET "/repos/$repo/issues?state=closed&type=issues&limit=1"`) → the agents did not fill
     it in. Missing → the agents work without project memory.

## Report

One short block per project: counts per label, what runs now, each blocked issue with its one-line reason and link (`http://gitea:3000/$repo/issues/$n`), specs with `done/total` stories and spend. Then drift, one line per project that has any (in sync projects are not listed), then service problems, if any. For a blocked issue, offer the `forge-unblock` skill; don't fix it here.
