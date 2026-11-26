#!/usr/bin/env bash
set -euo pipefail
usage="usage: merge-result.sh REPO BASE_REF HEAD_REF   print the commit that is HEAD_REF merged into BASE_REF, the tree a pull request becomes once it lands and so the tree CI checks; the same two commits always give the same result commit; exits 1 and says to update the branch onto main when the two conflict, 2 on any other failure"
[ $# = 3 ] || { echo "$usage" >&2; exit 2; }
repo=$1
base=$(git -C "$repo" rev-parse --verify --quiet "$2^{commit}") || { echo "merge-result: $2 is not a commit in $repo" >&2; exit 2; }
head=$(git -C "$repo" rev-parse --verify --quiet "$3^{commit}") || { echo "merge-result: $3 is not a commit in $repo" >&2; exit 2; }

if git -C "$repo" merge-base --is-ancestor "$base" "$head"; then
  echo "$head"
  exit 0
fi

wt=$(mktemp -d "${TMPDIR:-/tmp}/ci-merge.XXXXXX") || { echo "merge-result: could not make a scratch directory" >&2; exit 2; }
trap 'git -C "$repo" worktree remove --force "$wt" 2>/dev/null || rm -rf "$wt"; git -C "$repo" worktree prune' EXIT
git -C "$repo" worktree add -q --detach "$wt" "$base" || { echo "merge-result: could not check out $2" >&2; exit 2; }

date=$(git -C "$repo" log -1 --format=%cI "$head")
export GIT_AUTHOR_NAME=ci GIT_AUTHOR_EMAIL=ci@invalid GIT_AUTHOR_DATE=$date
export GIT_COMMITTER_NAME=ci GIT_COMMITTER_EMAIL=ci@invalid GIT_COMMITTER_DATE=$date
if ! why=$(git -C "$wt" -c commit.gpgsign=false -c core.hooksPath=/dev/null \
    merge -q --no-ff -m "Merge ${head:0:12} into ${base:0:12}" "$head" 2>&1); then
  if [ -n "$(git -C "$wt" ls-files -u)" ]; then
    echo "merge-result: ${3} conflicts with ${2} in: $(git -C "$wt" diff --name-only --diff-filter=U | tr '\n' ' ')" >&2
    echo "update your branch onto main" >&2
    exit 1
  fi
  echo "merge-result: merging $3 into $2 failed for a reason other than a conflict: $why" >&2
  exit 2
fi
git -C "$wt" rev-parse HEAD
