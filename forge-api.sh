#!/usr/bin/env bash
# Calls the forge's Gitea API as the admin and prints the response body; fails on an HTTP error.
#   ./forge-api.sh METHOD PATH [JSON | @file]      e.g. ./forge-api.sh GET /repos/forge/app/labels
# The skills use it instead of a shell function: skill arguments would replace a function's $1/$2/$3.
set -euo pipefail
[ $# -ge 2 ] || { echo "usage: $0 METHOD PATH [JSON | @file]" >&2; exit 2; }
cd "$(dirname "$0")"
args=(-sf -X "$1" "http://localhost:3000/api/v1$2" -u "forge:$(cat secrets/gitea-admin-password)" -H 'Content-Type: application/json')
[ $# -ge 3 ] && args+=(--data-binary "$3")
curl "${args[@]}"
