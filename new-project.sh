#!/usr/bin/env bash
# Create a forge project: Gitea repo, agents' deploy key, protected CI workflow, `forge` topic.
# With a spec file (first line `# Title`, then the body), also open it as a `spec` issue once the orchestrator is ready.
# Usage: ./new-project.sh NAME [SPEC_FILE]
set -euo pipefail
cd "$(dirname "$0")"

name=${1:?usage: $0 NAME [SPEC_FILE]}
spec=${2:-}
[ -z "$spec" ] || [ -f "$spec" ] || { echo "no such spec file: $spec" >&2; exit 1; }
owner=forge
repo="/repos/$owner/$name"

# Admin login: creating a repo needs the write:user scope, which the orchestrator token doesn't get.
api() {
  curl -sf -X "$1" "http://gitea:3000/api/v1$2" -u "forge:$(cat secrets/gitea-admin-password)" \
    -H 'Content-Type: application/json' ${3:+-d "$3"}
}
json() { python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$1"; }

api POST /user/repos "{\"name\": $(json "$name"), \"auto_init\": true}" >/dev/null
api POST "$repo/keys" "{\"title\": \"agents\", \"key\": $(json "$(cat secrets/deploy_key.pub)"), \"read_only\": false}" >/dev/null
# Before the topic: once the orchestrator sees the project, it protects main against every push.
api POST "$repo/contents/.gitea/workflows/ci.yml" \
  "{\"message\": \"ci: add workflow\", \"content\": \"$(base64 -w0 templates/ci.yml)\"}" >/dev/null
api POST "$repo/contents/CLAUDE.md" \
  "{\"message\": \"docs: add the agents' CLAUDE.md\", \"content\": \"$(base64 -w0 templates/CLAUDE.md)\"}" >/dev/null
api PUT "$repo/topics/forge" >/dev/null
echo "created http://gitea:3000/$owner/$name"
[ -n "$spec" ] || exit 0

# The orchestrator creates the labels on its next poll.
for _ in $(seq 60); do
  label=$(api GET "$repo/labels" | python3 -c 'import json, sys; print(next((l["id"] for l in json.load(sys.stdin) if l["name"] == "spec"), ""))')
  [ -n "$label" ] && break
  sleep 5
done
[ -n "$label" ] || { echo "no 'spec' label after 5 minutes: is the orchestrator running? (docker compose logs orchestrator)" >&2; exit 1; }

title=$(head -n1 "$spec" | sed 's/^# *//')
body=$(tail -n +2 "$spec")
number=$(api POST "$repo/issues" "{\"title\": $(json "$title"), \"body\": $(json "$body"), \"labels\": [$label]}" |
  python3 -c 'import json, sys; print(json.load(sys.stdin)["number"])')
echo "spec opened: http://gitea:3000/$owner/$name/issues/$number"
