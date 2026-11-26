#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

assert() {
  local name=$1 want=$2 got=$3
  if [ "$want" != "$got" ]; then
    echo "FAIL $name: want '$want', got '$got'" >&2
    fail=1
  else
    echo "ok   $name"
  fi
}

mkdir -p "$work/api/scripts" "$work/api/internal/lib" "$work/api/internal/other" "$work/api/internal/shared/clock" "$work/bin"
printf 'package clock\n' >"$work/api/internal/shared/clock/c.go"
cp -r "$here/lintdoors" "$work/api/scripts/lintdoors"
cp "$here/unexport-door.sh" "$work/api/scripts/unexport-door.sh"
printf 'module example/api\n\ngo 1.26\n' >"$work/api/go.mod"
printf 'package lib\n\nfunc Open() {}\n\nfunc HTTPClient() {}\n\nfunc New() {}\n\nfunc Used() {}\n' >"$work/api/internal/lib/lib.go"
printf 'package other\n\nimport "example/api/internal/lib"\n\nvar _ = lib.Used\n' >"$work/api/internal/other/o.go"

cat >"$work/bin/gopls" <<'S'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_LOG"
[ "${STUB_REFUSE:-}" = "$4" ] && { echo "refused by stub" >&2; exit 1; }
exit 0
S
chmod +x "$work/bin/gopls"
export STUB_LOG="$work/calls"
export PATH="$work/bin:$PATH"
: >"$STUB_LOG"

run() { (cd "$work/api" && bash scripts/unexport-door.sh "$@" 2>"$work/err"); }

set +e
run lib >"$work/out"
code=$?
set -e
assert "renames the unused names, acronym-aware, refusing a predeclared target" "1" "$code"
assert "an acronym lowers to a whole word" "1" "$(grep -c ' httpClient$' "$STUB_LOG")"
assert "Open lowers its first letter" "1" "$(grep -c ' open$' "$STUB_LOG")"
assert "a name used outside is left alone" "0" "$(grep -c ' used$' "$STUB_LOG")"
assert "New is refused before gopls sees it" "0" "$(grep -c ' new$' "$STUB_LOG")"
assert "the refusal names the name" "1" "$(grep -c 'New at .*predeclared' "$work/err")"

set +e
run bakoff >/dev/null
code=$?
set -e
assert "a misspelled module exits 2" "2" "$code"

for bad in plat shared shared/ nonesuch/x; do
  set +e
  run "$bad" >/dev/null
  code=$?
  set -e
  assert "$bad is not a module, exit 2" "2" "$code"
done

set +e
run lib/ >/dev/null
code=$?
set -e
assert "a trailing slash is accepted for a module" "1" "$code"

: >"$STUB_LOG"
set +e
STUB_REFUSE=open run lib >/dev/null
code=$?
set -e
assert "a gopls refusal exits 1 and says why" "1" "$code"
assert "the gopls reason is reported" "1" "$(grep -c 'refused by stub' "$work/err")"

rm "$work/api/go.mod"
set +e
run lib >/dev/null
code=$?
set -e
assert "a lintdoors failure exits 2, not a quiet success" "2" "$code"

exit "$fail"
