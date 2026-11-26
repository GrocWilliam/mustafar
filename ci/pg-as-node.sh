#!/bin/bash
# initdb, pg_ctl and postgres refuse to run as root, and CI jobs run as root while agents run as node: as root, run
# the real command as node, after giving node the temporary directories it names (data dir parent, -l log, -k socket).
real=/usr/lib/postgresql/16/bin/$(basename "$0")
[ "$(id -u)" = 0 ] || exec "$real" "$@"
tmp=${TMPDIR:-/tmp}
for word in $*; do # unquoted on purpose: paths inside -o "-k /tmp/x ..." are words too
  path=${word#*=}
  case "$path" in "$tmp"/?* | /tmp/?*) ;; *) continue ;; esac
  # shortcut: paths under /tmp or $TMPDIR only, chowned one level; widen if a test keeps its cluster elsewhere
  [ -e "$path" ] || path=$(dirname "$path")
  [ -e "$path" ] && [ "$path" != /tmp ] && [ "$path" != "$tmp" ] && chown -R node:node "$path"
done
exec setpriv --reuid=node --regid=node --init-groups "$real" "$@"
