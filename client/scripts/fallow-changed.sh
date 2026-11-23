#!/usr/bin/env bash
set -uo pipefail
usage="usage: fallow-changed.sh BASE_REF   checks the module boundaries, part boundaries and duplicated code in the client files changed since BASE_REF"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
cd "$(dirname "$0")/.." || exit 3
git rev-parse --verify --quiet "$1^{commit}" >/dev/null || { echo "fallow-changed: cannot resolve $1 to a commit, and fallow would then check the whole tree" >&2; exit 2; }
fallow=node_modules/.bin/fallow
any_duplication=0.0001

broken=0
run() {
  "$fallow" "$@" --changed-since "$base"
  case $? in
    0) ;;
    1) broken=1 ;;
    *) echo "fallow-changed: fallow $* could not run (error above)" >&2; exit 3 ;;
  esac
}
base=$1
run dead-code -c fallow/modules.json --boundary-violations
run dead-code -c fallow/parts.json --boundary-violations
run dupes --threshold "$any_duplication"
[ $broken = 0 ] || { echo "fallow-changed: the findings above break the client module rules. Read client/src/README.md." >&2; exit 1; }
