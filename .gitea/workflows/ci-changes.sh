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
  flag backend '^(services/go-api/|scripts/uptime/|\.gitea/workflows/ci|\.github/workflows/(test-backend|nilaway))'
  flag mobile '^(apps/mobile/|\.gitea/workflows/ci|\.github/workflows/test-mobile)'
  flag overseer '^(services/overseer/|\.gitea/workflows/ci|\.github/workflows/nilaway)'
} | tee -a "${GITHUB_OUTPUT:-/dev/null}"
