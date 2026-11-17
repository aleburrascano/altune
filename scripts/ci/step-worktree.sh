#!/usr/bin/env bash
set -euo pipefail
repo=${ALTUNE_REPO:?the runner sets ALTUNE_REPO to its checkout}
deps_base=${1:?usage: step-worktree.sh <deps-base> <ref> [<dir>...]}
ref=${2:?usage: step-worktree.sh <deps-base> <ref> [<dir>...]}
shift 2
if [ "${ALTUNE_CI_OWNED:-}" = 1 ]; then
  bash "$repo/../refresh-deps.sh" "$deps_base"
fi
wt=$(mktemp -d "${TMPDIR:-/tmp}/ci-wf.XXXXXX")
trap 'git -C "$repo" worktree remove --force "$wt" 2>/dev/null || rm -rf "$wt"' EXIT
git -C "$repo" worktree add -q --detach "$wt" "$ref"
cd "$wt"
deps=$(bash scripts/worktree-deps.sh "$@")
eval "$deps"
