#!/usr/bin/env bash
# Mirror a forge project to an external git platform (GitHub, GitLab, Azure DevOps, any HTTPS git remote).
# Gitea push-mirrors every branch and tag on each commit and every 8h. One-way: the forge stays the source of truth.
# The remote repo must exist and be empty (or a mirror): Gitea force-pushes to it.
# Usage: MIRROR_TOKEN=... [MIRROR_USER=...] ./mirror.sh NAME REMOTE_URL
#   GitHub:       https://github.com/ORG/REPO.git                       token: PAT with contents:write
#   GitLab:       https://gitlab.com/GROUP/REPO.git                     token: PAT with write_repository, MIRROR_USER=oauth2
#   Azure DevOps: https://dev.azure.com/ORG/PROJECT/_git/REPO           token: PAT with Code (Read & write)
# Remove a mirror in Gitea: repo › Settings › Repository › Mirror settings.
set -euo pipefail
cd "$(dirname "$0")"

name=${1:?usage: MIRROR_TOKEN=... $0 NAME REMOTE_URL}
remote=${2:?usage: MIRROR_TOKEN=... $0 NAME REMOTE_URL}
: "${MIRROR_TOKEN:?set MIRROR_TOKEN to a token with write access to $remote}"
[[ $remote == https://* ]] || { echo "REMOTE_URL must be https://" >&2; exit 1; }
repo="http://localhost:3000/api/v1/repos/forge/$name"
auth="forge:$(cat secrets/gitea-admin-password)"

# The body goes through stdin so the token never shows in `ps`.
MIRROR_URL=$remote python3 -c 'import json, os; print(json.dumps({
  "remote_address": os.environ["MIRROR_URL"],
  "remote_username": os.environ.get("MIRROR_USER") or "forge",
  "remote_password": os.environ["MIRROR_TOKEN"],
  "interval": "8h0m0s", "sync_on_commit": True}))' |
  curl -sSf -X POST "$repo/push_mirrors" -u "$auth" -H 'Content-Type: application/json' -d @- >/dev/null
echo "forge/$name now mirrors to $remote"
curl -sf -X POST "$repo/push_mirrors-sync" -u "$auth" >/dev/null ||
  { echo "first push failed: check the URL, token and that the remote repo exists (docker compose logs gitea)" >&2; exit 1; }
