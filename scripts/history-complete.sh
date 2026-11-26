#!/usr/bin/env bash
set -euo pipefail
usage="usage: history-complete.sh   run in the repo; exits 1 when a shallow boundary sits in HEAD's own history, so the root commit is unknown and a whole-tree check would pass without looking; a shallow entry elsewhere (another branch's) does not matter"
[ $# = 0 ] || { echo "$usage" >&2; exit 2; }
[ "$(git rev-parse --is-shallow-repository)" = true ] || exit 0
shallow=$(git rev-parse --git-path shallow)
while read -r boundary; do
  [ -n "$boundary" ] || continue
  rc=0
  git merge-base --is-ancestor "$boundary" HEAD || rc=$?
  case $rc in
    0) echo "history-complete: this clone is shallow at ${boundary:0:12}, inside HEAD's history, so files added before it would go unchecked; run: git fetch --unshallow" >&2; exit 1 ;;
    1) ;;
    *) echo "history-complete: could not tell whether ${boundary:0:12} is in HEAD's history" >&2; exit 2 ;;
  esac
done <"$shallow"
