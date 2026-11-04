#!/usr/bin/env bash
# Stop hook, dev only: before the dev finishes, run what CI runs (templates/ci.yml). A failure keeps the dev working
# in the same session (exit 2, the reason on stderr), instead of costing a CI round and one of its attempts.
set -uo pipefail
[ "${ROLE:-}" = dev ] || exit 0
cat >/dev/null # the event JSON: nothing in it is needed
cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0

# ponytail: at most 2 refusals per run, then the dev may finish and explain (a contradiction in the tests, a missing
# tool); CI and the retry feedback take over. Raise MAX_CHECKS if devs give up too early.
max=${MAX_CHECKS:-2}
counter=/tmp/forge-stop-checks
count=$(cat "$counter" 2>/dev/null || echo 0)
[ "$count" -lt "$max" ] || exit 0
log=$(mktemp)

fail() {
  echo $((count + 1)) >"$counter"
  {
    echo "Blocked by the forge: $1 Fix it, commit, then finish. (check $((count + 1))/$max: after the last one you may finish anyway, and say in your summary what is still failing and why)"
    echo
    tail -n 60 "$log"
  } >&2
  exit 2
}

if [ -n "$(git status --porcelain)" ]; then
  git status --short >"$log"
  fail "you left uncommitted changes, and CI tests only what is committed. Commit your work; add build outputs to .gitignore instead of committing them."
fi
make test >"$log" 2>&1 || fail "\`make test\` fails, as it will in CI."
if make -n build >/dev/null 2>&1; then
  make build >"$log" 2>&1 || fail "\`make build\` fails, as it will in CI."
  export FORGE_PREBUILT=1
fi
for script in tests/acceptance/*/run.sh; do
  [ -e "$script" ] || continue
  bash "$script" >"$log" 2>&1 || fail "the acceptance test \`$script\` fails, as it will in CI."
done
exit 0
