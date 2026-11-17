#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/violations.sh
fail=0

[ -x ../node_modules/.bin/module-spec ] || { echo "violations.test.sh: the module-spec kit is not installed, run pnpm install at the repo root"; exit 1; }

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
root=$scratch/root
mkdir -p "$root/client/module-spec.baseline.d" "$root/services/go-api" "$root/client/node_modules/kit"
git -C "$root" init -q
echo node_modules >"$root/.gitignore"
for dir in client services/go-api client/node_modules/kit; do echo '{}' >"$root/$dir/module-spec.json"; done

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

run "$root/services"
check 0 "a subfolder of the checkout reads the whole checkout"
[ "$(wc -l <<<"$OUT")" = 3 ] || { echo "FAIL a subfolder argument should still print all 3 lines"; sed 's/^/    /' <<<"$OUT"; fail=1; }

cat >"$root/client/module-spec.baseline.d/0123456789ab.json" <<'X'
{ "removed": ["T2 src/shared/c/__tests__/d.test.ts mocks ../e"], "added": ["B1 src/features/a/z.ts moved here"], "clones": -1 }
X
run "$root"
check 0 "a baseline with a fragment prints"
expect_eq "$(cat <<'X'
{"rule":"B1","file":"client/src/features/a/z.ts","baseline":"client/module-spec.baseline.json","entry":"B1 src/features/a/z.ts moved here"}
{"rule":"C3","file":"client/src/features/a/b.ts","baseline":"client/module-spec.baseline.json","entry":"C3 src/features/a/b.ts imports src/shared/x/y.ts, behind x's door"}
{"rule":"B3","file":"services/go-api/internal/acquisition/adapters/f.go","baseline":"services/go-api/module-spec.baseline.json","entry":"B3 internal/acquisition/adapters/f.go imports os"}
X
)" "the effective baseline: a fragment's removed key leaves and its added key joins"

epipe=$( { bash "$script" "$root" | true; } 2>&1 )
[ -z "$epipe" ] || { echo "FAIL a reader that closes early should leave stderr empty"; sed 's/^/    /' <<<"$epipe"; fail=1; }

printf '{ "violations": ["lonely"] }\n' >"$root/services/go-api/module-spec.baseline.json"
run "$root"
check 1 "an entry without a rule and file exits 1"
expect_out "violations: the services/go-api baseline has an entry that names no rule and file: lonely"

printf 'not json\n' >"$root/services/go-api/module-spec.baseline.json"
run "$root"
check 1 "a broken baseline exits 1"
expect_out "violations: module-spec baseline --list --root services/go-api exited 2: module-spec: module-spec.baseline.json is not JSON"

printf 'not json\n' >"$root/client/module-spec.baseline.d/0123456789ab.json"
run "$root"
check 1 "a broken fragment exits 1"
expect_out "module-spec.baseline.d/0123456789ab.json is not JSON"

run "$scratch/missing"
check 2 "a folder that does not exist exits 2"
expect_out "violations: $scratch/missing is not a folder"

mkdir -p "$scratch/nogit"
run "$scratch/nogit"
check 1 "a folder outside git exits 1"
expect_out "violations: git rev-parse in $scratch/nogit exited 128"

mkdir -p "$scratch/nokit/scripts"
cp violations.mjs violations.sh module-spec.sh "$scratch/nokit/scripts/"
OUT=$(bash "$scratch/nokit/scripts/violations.sh" "$root" 2>&1); CODE=$?
check 3 "no kit exits 3"
expect_out "violations: the module-spec kit is not installed"

[ $fail = 0 ] && echo "violations.test.sh: all cases pass"
exit $fail
