#!/usr/bin/env bash
set -euo pipefail
usage="usage: pr-not-empty.sh BASE_REF   fails when HEAD has no changes beyond BASE_REF (git diff BASE_REF...HEAD is empty), the sign of a PR whose commit was lost"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
base=$1
git rev-parse --verify --quiet "$base^{commit}" >/dev/null || { echo "::error::pr-not-empty: $base is not a commit here, so nothing was checked" >&2; exit 2; }
rc=0
git diff --quiet "$base...HEAD" || rc=$?
case $rc in
  0)
    echo "::error::This PR has no changes beyond $base: $base...HEAD is empty, either no commits or commits that cancel out. A commit may have been lost to an amend or reset that ran before a background commit hook finished. Check git log $base..HEAD, recommit, and push again."
    exit 1
    ;;
  1) echo "pr-not-empty: HEAD has changes beyond $base" ;;
  *) echo "::error::pr-not-empty: git diff $base...HEAD failed with exit $rc, so nothing was checked" >&2; exit 2 ;;
esac
