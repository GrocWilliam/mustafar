---
name: forge-unblock
description: Diagnose a blocked forge issue (`us-blocked` story or spec) and retry it - read why it stopped, fix the story, spec, budget or infrastructure, then relabel it. Use for "unblock", "retry", "why is #N blocked", "the forge is stuck".
---

# Forge unblock

`us-blocked` is the forge's only point of human intervention. Retrying starts paid agent runs again, so **say what you will change and get the user's OK before editing an issue or relabelling it.**

Work from the forge root. Never print the admin password or `.env` values.

API calls go through `./forge-api.sh METHOD PATH [JSON | @file]` (prints the body, fails on an HTTP error).

## 1. Find out why

Read the issue and all its comments (`./forge-api.sh GET "/repos/$repo/issues/$n"` and `.../comments`): the last `⛔ Blocked:` comment gives the reason, the `🤖 Agent` summaries and `🔁 Attempt` feedback show the history. Open the PR and CI run if they matter (`http://localhost:3000/$repo/pulls`, `.../actions`). Then classify:

| Reason says | Usually means | Fix |
|---|---|---|
| `still rejected after 3 attempts` | the story is ambiguous, too big, or its tests contradict it | sharpen the story's criteria. Splitting it means new `us-todo` issues ending with `Part of #spec` and `Depends on #n`, and closing the original; the spec only tracks the stories of its plan marker, so it may close before the new ones finish |
| `the tester changed files outside` / `wrote no acceptance tests` | tester misread the story | make the story's interface and criteria explicit |
| `orchestrator error` / `no CI result after` | infrastructure: runner down, Gitea or Docker error | `docker compose ps`, `docker compose logs --tail 50 runner orchestrator`; fix, then retry unchanged |
| `staging deployment failed` | merged, then reverted: the image did not build or `/health` failed | read the build/health error; add the deployment requirement to the story |
| `invalid plan` / planner failed (on a spec) | spec too vague, or more than 20 stories | sharpen or split the spec |
| `over budget` (on a spec) | `MAX_COST_PER_SPEC` reached | ask the user before raising it |

## 2. Fix

- Edit a story or spec body: `./forge-api.sh PATCH "/repos/$repo/issues/$n" '{"body": ...}'` (JSON built with `python3` so quotes and newlines survive). Keep the `---`, `Part of #` and `Depends on #` lines at the end of a story.
- Budget: for one spec, post a comment ending with `<!-- forge:budget=35.00 -->` on it (what the forge UI's budget form does; no restart). For every spec, change `MAX_COST_PER_SPEC` in `.env`, then `docker compose up -d orchestrator` (running stories go back to the queue).

## 3. Retry

Get the label ids from `./forge-api.sh GET "/repos/$repo/labels"`. Remove `us-blocked` (`./forge-api.sh DELETE "/repos/$repo/issues/$n/labels/$BLOCKED_ID"`), then add the label to resume from (`./forge-api.sh POST "/repos/$repo/issues/$n/labels" "{\"labels\": [$ID]}"`):

- a story → `us-todo`: it resumes its branch `agent/us-<n>` when the dev had already worked on it (the tester is skipped and its tests stay frozen), otherwise it starts over from the tester. If CI had failed on the branch, the orchestrator merges `main` into it for a fresh CI run; when `main` has not moved there is nothing to merge, so a retry for a flaky CI failure goes straight to the dev with the old failure (rerun the job in the Gitea UI first, or let `main` move). Delete the branch to force a fresh start (`./forge-api.sh DELETE "/repos/$repo/branches/agent%2Fus-$n"`), e.g. when the tests themselves were wrong;
- a spec whose planning failed → `spec`: planned again (a failed plan's stories were closed);
- a spec over budget → `spec-active`, then `us-todo` on each of its blocked stories.

Always remove `us-blocked` first: the orchestrator only swaps the label it expects, so a leftover one stays forever. Report what you changed, and that the forge UI (http://localhost:8000) and `forge-status` show the progress. The UI also has these retry and budget buttons, for the user to use directly.
