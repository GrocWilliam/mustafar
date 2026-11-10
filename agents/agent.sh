#!/usr/bin/env bash
# One-shot agent: clone the repo, run Claude Code headless on the US as $ROLE, push the branch.
# Stdout carries only Claude's stream-json (parsed by the orchestrator); everything else goes to stderr.
set -euo pipefail

branch="agent/us-${ISSUE_NUMBER}"
[ "$ROLE" != integrator ] || branch="agent/spec-${ISSUE_NUMBER}"

# The key lives on disk only while git needs it, so Claude cannot read it from ~/.ssh.
# Not exported: claude does not inherit it (a same-user /proc read could still find it).
key="$DEPLOY_KEY"
unset DEPLOY_KEY
mkdir -p ~/.ssh
write_key() { (umask 077; printf '%s\n' "$key" > ~/.ssh/id_ed25519); }
write_key
export GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new"

git clone "$REPO_URL" workspace >&2
cd workspace
# Local only: keep dependency and build folders out of any commit, the partial-work one below included.
printf '%s\n' node_modules/ .svelte-kit/ __pycache__/ .venv/ >> .git/info/exclude
git config user.name "forge-agent-${ROLE}"
git config user.email "agent-${ROLE}@forge.local"
if [ "$ROLE" = revert ]; then # no Claude: the orchestrator undoes a merge whose deployment failed
  git revert --no-edit "$REVERT_SHA" >&2
  git push --force origin "HEAD:refs/heads/${REVERT_BRANCH}" >&2 # main is protected: the orchestrator merges this branch

  exit 0
fi
case "$ROLE" in
  planner | architect) ;;                                        # read main, write nothing
  tester) git checkout -b "$branch" "origin/$BASE_BRANCH" >&2 ;; # each tester run restarts the branch from its target
  *) git checkout "$branch" >&2 ;;                               # dev and reviewer continue the tester's branch, the integrator the orchestrator's copy of the spec
esac
start=$(git rev-parse HEAD)
rm ~/.ssh/id_ed25519
case "$ROLE" in planner | architect | reviewer) unset key ;; esac # read-only roles never push

{
  sed "s|{{BASE_BRANCH}}|${BASE_BRANCH:-}|g" "/prompts/${ROLE}.md"
  printf '\n\n## Issue #%s: %s\n\n%s\n' "$ISSUE_NUMBER" "$ISSUE_TITLE" "$ISSUE_BODY"
  if [ -n "${FEEDBACK:-}" ]; then
    printf '\n## Feedback: why your previous attempt was rejected\n\n%s\n' "$FEEDBACK"
  fi
} > /tmp/task.md

# A stalled run must not hold an agent slot until the orchestrator restarts.
status=0
timeout 45m claude -p "$(cat /tmp/task.md)" ${MODEL:+--model "$MODEL"} --permission-mode bypassPermissions \
  --output-format stream-json --max-turns "${MAX_TURNS:-30}" --verbose || status=$?

# The planner, the architect and the reviewer only read: their output is their final message.
case "$ROLE" in planner | architect | reviewer) exit "$status" ;; esac

if [ "$status" -ne 0 ]; then
  # Partial tests are worse than none: a failed tester pushes nothing.
  [ "$ROLE" = dev ] || exit "$status"
  # A dev run cut short (turn limit, timeout) keeps its work, so the next attempt continues from it.
  # Never its edits to the frozen tests: the next attempt would inherit a rejected change.
  git add -A -- . ':(exclude)tests/acceptance' >&2
  if ! git diff --cached --quiet; then
    git commit -q -m "wip: partial work, agent stopped (exit $status)" >&2
  fi
fi

if [ "$(git rev-parse HEAD)" = "$start" ]; then
  echo "agent produced no commit" >&2
  [ "$status" -ne 0 ] && exit "$status"
  # A dev with nothing to add (a tests-only story the tester already did) goes on: CI and the reviewer judge the branch.
  # A retester that rejects every reported conflict changes nothing, on purpose.
  [ "$ROLE" = dev ] || [ "$ROLE" = retester ] && exit 0
  exit 3
fi
write_key
if [ "$ROLE" = tester ]; then
  git push --force -u origin "$branch" >&2 # replaces the branch of an earlier run of this US
else
  git push origin "$branch" >&2 # never forced; the orchestrator's frozen-tests check catches rewrites anyway
fi
exit "$status" # a dev run cut short still fails: the orchestrator retries it
