#!/usr/bin/env bash
# One-shot agent: clone the repo, run Claude Code headless on the US as $ROLE, push the branch.
# Stdout carries only Claude's stream-json (parsed by the orchestrator); everything else goes to stderr.
set -euo pipefail

branch="agent/us-${ISSUE_NUMBER}"

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
git config user.name "forge-agent-${ROLE}"
git config user.email "agent-${ROLE}@forge.local"
if [ "$ROLE" = revert ]; then # no Claude: the orchestrator undoes a merge whose deployment failed
  git revert --no-edit "$REVERT_SHA" >&2
  git push --force origin "HEAD:refs/heads/${REVERT_BRANCH}" >&2 # main is protected: the orchestrator merges this branch

  exit 0
fi
case "$ROLE" in
  planner) ;;                               # reads main, writes nothing
  tester) git checkout -b "$branch" >&2 ;; # each tester run restarts the branch from main
  *) git checkout "$branch" >&2 ;;         # dev and reviewer continue the tester's branch
esac
start=$(git rev-parse HEAD)
rm ~/.ssh/id_ed25519
case "$ROLE" in planner | reviewer) unset key ;; esac # read-only roles never push

{
  cat "/prompts/${ROLE}.md"
  printf '\n\n## Issue #%s: %s\n\n%s\n' "$ISSUE_NUMBER" "$ISSUE_TITLE" "$ISSUE_BODY"
  if [ -n "${FEEDBACK:-}" ]; then
    printf '\n## Feedback: why your previous attempt was rejected\n\n%s\n' "$FEEDBACK"
  fi
} > /tmp/task.md

# A stalled run must not hold an agent slot until the orchestrator restarts.
timeout 45m claude -p "$(cat /tmp/task.md)" --permission-mode bypassPermissions \
  --output-format stream-json --max-turns 30 --verbose

# The planner and the reviewer only read: their output is their final message.
case "$ROLE" in planner | reviewer) exit 0 ;; esac

if [ "$(git rev-parse HEAD)" = "$start" ]; then
  echo "agent produced no commit" >&2
  exit 3
fi
write_key
if [ "$ROLE" = tester ]; then
  git push --force -u origin "$branch" >&2 # replaces the branch of an earlier run of this US
else
  git push origin "$branch" >&2 # never forced; the orchestrator's frozen-tests check catches rewrites anyway
fi
