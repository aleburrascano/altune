#!/usr/bin/env bash
set -euo pipefail
repo=${ALTUNE_REPO:-/home/ubuntu/projects/altune}
ref=$1
find "${TMPDIR:-/tmp}" /tmp -maxdepth 1 -name 'ci-wf.*' -mmin +360 -print0 2>/dev/null \
  | xargs -0 -r -n1 git -C "$repo" worktree remove --force 2>/dev/null || true
git -C "$repo" worktree prune
wt=$(mktemp -d "${TMPDIR:-/tmp}/ci-wf.XXXXXX")
git -C "$repo" worktree add -q --detach "$wt" "$ref" || { rm -rf "$wt"; exit 1; }
echo "WT=$wt" >>"$GITHUB_ENV"
node22=$(ls -d "$HOME"/.nvm/versions/node/v22.*/bin 2>/dev/null | sort -V | tail -1)
[ -n "$node22" ] && echo "$node22" >>"$GITHUB_PATH"
echo "$(go env GOPATH)/bin" >>"$GITHUB_PATH"
echo "worktree $wt at $(git -C "$wt" rev-parse HEAD)"
