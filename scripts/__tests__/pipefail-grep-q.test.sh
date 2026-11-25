#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/pipefail-grep-q.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT

OUT=""; CODE=0
run() { OUT=$(bash "$script" "$@" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL output should mention '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' 'printf "%s\n" "$x" | grep -qx HARD && echo hit' >"$scratch/piped.sh"
printf '%s\n' '#!/usr/bin/env bash' 'set -o pipefail' 'docker ps | grep -qxF name' >"$scratch/piped-o.sh"
printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' 'grep -qx HARD <<<"$x" && echo hit' 'printf "%s\n" "$x" | grep -x HARD' >"$scratch/herestring.sh"
printf '%s\n' '#!/usr/bin/env bash' 'set -eu' 'printf "%s\n" "$x" | grep -qx HARD' >"$scratch/no-pipefail.sh"

run "$scratch/piped.sh"
check 1 "a pipe into grep -qx under pipefail fails"
expect_out "piped.sh:3:"

run "$scratch/piped-o.sh"
check 1 "set -o pipefail with a pipe into grep -qxF fails"

run "$scratch/herestring.sh"
check 0 "a here-string and a pipe into grep without -q pass"

run "$scratch/no-pipefail.sh"
check 0 "a script without pipefail passes"

run "$scratch/herestring.sh" "$scratch/piped.sh"
check 1 "one offending file among several fails"

repo=$scratch/repo
mkdir -p "$repo/deep/er"
git init -qb main "$repo"
cp "$scratch/piped.sh" "$repo/piped.sh"
cp "$scratch/herestring.sh" "$repo/deep/clean.sh"
git -C "$repo" add -A

run_in() { local dir=$1; shift; OUT=$(cd "$dir" && bash "$script" "$@" 2>&1); CODE=$?; }
run_in "$repo"
check 1 "with no arguments a violation in the checkout is found from its root"
expect_out "piped.sh:3:"
run_in "$repo/deep/er"
check 1 "with no arguments a violation in the checkout is found from a subdirectory too"
expect_out "piped.sh:3:"
run_in "$scratch"
check 2 "with no arguments outside a git checkout is an error, not a pass"
expect_out "nothing was checked"

exit $fail
