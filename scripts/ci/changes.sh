#!/usr/bin/env bash
set -euo pipefail
if [ -n "${1:-}" ] && base=$(git merge-base "$1" HEAD 2>/dev/null); then
  changed=$(git diff --name-only "$base" HEAD)
else
  changed=all
fi
flag() {
  if [ "$changed" = all ] || grep -qE "$2" <<<"$changed"; then echo "$1=true"; else echo "$1=false"; fi
}
{
  flag backend '^(services/go-api/|scripts/uptime/|services/supabase/|\.gitea/workflows/ci\.yml|scripts/ci/(changes|checkout|cleanup|node-deps|postgres)\.sh|scripts/ci/(test-backend|nilaway))'
  flag mobile '^(client/|\.gitea/workflows/ci\.yml|scripts/ci/(changes|checkout|cleanup|node-deps|postgres)\.sh|scripts/ci/test-mobile)'
  flag overseer '^(services/overseer/|\.gitea/workflows/ci\.yml|scripts/ci/(changes|checkout|cleanup|node-deps|postgres)\.sh|scripts/ci/nilaway)'
} | tee -a "${GITHUB_OUTPUT:-/dev/null}"
