---
name: forge-story
description: Add a single user story to an existing forge project, skipping the planner, labelled `priority` so it takes the next free agent slot before the queued stories. Use for "add a US", "add a story", "urgent story", "fix X first", "add this with top priority". For a feature that needs several stories, use forge-spec.
---

# Forge story

A story is a Gitea issue labelled `us-todo`. Opening one starts paid agent runs (tester, dev up to 3 attempts, reviewer, merge, staging deploy). So **show the final story and get the user's OK before opening it.** With the `priority` label, the orchestrator starts it before every other ready `us-todo` story. Running agents are not interrupted: it takes the next free slot.

Work from the forge root. Never print the admin password or `.env` values.

API calls go through `./forge-api.sh METHOD PATH [JSON | @file]` (prints the body, fails on an HTTP error).

## 1. Write the story

Read the project first (`git clone http://localhost:3000/$repo.git` into the scratchpad): its `CLAUDE.md`, the code the story touches, and `tests/acceptance/`. Nobody answers the agents' questions, so the story stands on its own:

- **Behavior, then `Acceptance criteria:`** followed by a `-` list a test can check: requests and exact responses, error messages, what the page shows (`data-testid`). Name the interfaces (file, function, endpoint) in the terms the code already uses. One dev session of about 30 turns: split anything bigger into stories linked by `Depends on`.
- **Frozen tests**: only the tester may change existing acceptance tests, and the dev is rejected if it touches them. When the change breaks one, for example a test comparing the whole JSON of an object that gains a field, name the file and the edit the tester makes, and add "The dev never edits them."
- **Existing rules**: check the change does not contradict another story's tests, such as a validation cap or a migration count. If it does, say how the tester reaches the behavior without breaking them.

End the body with these lines:

```
---
Part of #<spec>
Depends on #<n>      (only if it needs an open story merged first)
Complexity: <low|medium|high>
```

`Complexity` picks the agents' model (the forge's `MODEL_LOW`/`MEDIUM`/`HIGH` settings): `low` for mechanical work, `medium` (the default when missing) for most stories, `high` only for design-heavy work such as a new data model, concurrency or security.

`Part of` is required: the forge UI, its retry button and the spend per spec treat an issue without it as a spec. Use the spec the story belongs to, otherwise the project's latest spec. Its cost counts toward that spec's budget, but the spec does not wait for the story before closing.

## 2. Open it

Get the label ids (`./forge-api.sh GET "/repos/$repo/labels"`). If `priority` is missing, the orchestrator predates it: redeploy with the `forge-setup` skill first, or open the story with `us-todo` only. Then create the issue, building the JSON with `python3` so quotes and newlines survive:

`./forge-api.sh POST "/repos/$repo/issues" "{\"title\": ..., \"body\": ..., \"labels\": [US_TODO_ID, PRIORITY_ID]}"`

## 3. Hand over

Give the story link, and mention that the forge UI's "Add a story" form on each spec card does the same without the checks above. It starts on the next poll once a slot is free and its `Depends on` issues are closed. Its agents comment on it as they go, the forge UI (http://localhost:8000) and `forge-status` show progress, and `forge-unblock` handles it if it gets blocked.
