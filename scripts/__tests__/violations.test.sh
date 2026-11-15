#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/violations.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
root=$scratch/root
mkdir -p "$root/client" "$root/services/go-api" "$root/client/node_modules/kit"

OUT=""; CODE=0
run() { OUT=$(bash "$script" "$@" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_eq() {
  [ "$OUT" = "$1" ] || { echo "FAIL $2"; echo "  want:"; sed 's/^/    /' <<<"$1"; echo "  got:"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL output should mention '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

cat >"$root/client/module-spec.baseline.json" <<'X'
{ "violations": ["C3 src/features/a/b.ts imports src/shared/x/y.ts, behind x's door", "T2 src/shared/c/__tests__/d.test.ts mocks ../e"], "clones": 2 }
X
cat >"$root/services/go-api/module-spec.baseline.json" <<'X'
{ "violations": ["B3 internal/acquisition/adapters/f.go imports os"], "clones": 0 }
X
printf '{ "violations": ["X1 ignored.ts in node_modules"] }\n' >"$root/client/node_modules/kit/module-spec.baseline.json"

run "$root"
check 0 "well-formed baselines print"
expect_eq "$(cat <<'X'
{"rule":"C3","file":"client/src/features/a/b.ts","baseline":"client/module-spec.baseline.json","entry":"C3 src/features/a/b.ts imports src/shared/x/y.ts, behind x's door"}
{"rule":"T2","file":"client/src/shared/c/__tests__/d.test.ts","baseline":"client/module-spec.baseline.json","entry":"T2 src/shared/c/__tests__/d.test.ts mocks ../e"}
{"rule":"B3","file":"services/go-api/internal/acquisition/adapters/f.go","baseline":"services/go-api/module-spec.baseline.json","entry":"B3 internal/acquisition/adapters/f.go imports os"}
X
)" "one JSON line per baselined violation, repo-relative, node_modules skipped"

epipe=$( { bash "$script" "$root" | true; } 2>&1 )
[ -z "$epipe" ] || { echo "FAIL a reader that closes early should leave stderr empty"; sed 's/^/    /' <<<"$epipe"; fail=1; }

printf '{ "violations": ["lonely"] }\n' >"$root/services/go-api/module-spec.baseline.json"
run "$root"
check 1 "an entry without a rule and file exits 1"
expect_out "violations: services/go-api/module-spec.baseline.json has an entry that names no rule and file: lonely"

printf 'not json\n' >"$root/services/go-api/module-spec.baseline.json"
run "$root"
check 1 "a broken baseline exits 1"
expect_out "violations: services/go-api/module-spec.baseline.json is not JSON"

[ $fail = 0 ] && echo "violations.test.sh: all cases pass"
exit $fail
