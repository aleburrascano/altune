#!/usr/bin/env bash
set -uo pipefail
[ -n "${PG_CID:-}" ] && docker rm -f "$PG_CID" >/dev/null 2>&1
[ -n "${WT:-}" ] && [ -d "$WT" ] && git -C "${ALTUNE_REPO:-/home/ubuntu/projects/altune}" worktree remove --force "$WT"
exit 0
