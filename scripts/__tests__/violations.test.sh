#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/violations.sh
repo=$(cd .. && pwd)
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
root=$scratch/root
mkdir -p "$root/client/baselines" "$root/services/go-api"

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

cat >"$root/client/baselines/consistency.baseline" <<'X'
MC-1 src/a/b.ts '@shared/x' import is restricted (MC-1).

MC-4b src/c/D.tsx Don't import 'Pressable'.
X
printf 'N3 src/f/g.tsx is not PascalCase\nN4 src/h/i.ts\n' >"$root/client/baselines/module-shape.baseline"
printf '__tests__/a.test.tsx @shared/auth/supabaseClient\n' >"$root/client/baselines/mock-ratchet.baseline"
printf 'G1 internal/observe/handler unexpected root entry\n\nG2 internal/d/r.go:12 domain imports uuid\n' >"$root/services/go-api/module-shape.baseline"

run "$root"
check 0 "well-formed baselines print"
expect_eq "$(cat <<'X'
{"rule":"MC-1","file":"client/src/a/b.ts","baseline":"client/baselines/consistency.baseline","entry":"MC-1 src/a/b.ts '@shared/x' import is restricted (MC-1)."}
{"rule":"MC-4b","file":"client/src/c/D.tsx","baseline":"client/baselines/consistency.baseline","entry":"MC-4b src/c/D.tsx Don't import 'Pressable'."}
{"rule":"N3","file":"client/src/f/g.tsx","baseline":"client/baselines/module-shape.baseline","entry":"N3 src/f/g.tsx is not PascalCase"}
{"rule":"N4","file":"client/src/h/i.ts","baseline":"client/baselines/module-shape.baseline","entry":"N4 src/h/i.ts"}
{"rule":"MOCK","file":"client/__tests__/a.test.tsx","baseline":"client/baselines/mock-ratchet.baseline","entry":"__tests__/a.test.tsx @shared/auth/supabaseClient"}
{"rule":"G1","file":"services/go-api/internal/observe/handler","baseline":"services/go-api/module-shape.baseline","entry":"G1 internal/observe/handler unexpected root entry"}
{"rule":"G2","file":"services/go-api/internal/d/r.go","baseline":"services/go-api/module-shape.baseline","entry":"G2 internal/d/r.go:12 domain imports uuid"}
X
)" "exact JSON lines, empty lines skipped"

epipe=$( { bash "$script" "$root" | true; } 2>&1 )
[ -z "$epipe" ] || { echo "FAIL a reader that closes early should leave stderr empty"; sed 's/^/    /' <<<"$epipe"; fail=1; }

printf 'MC-1 src/a/b.ts ok\nbroken line\n' >"$root/client/baselines/consistency.baseline"
run "$root"
check 1 "a malformed line exits 1"
expect_out "violations: client/baselines/consistency.baseline:2 does not match"

printf 'MC-1 src/a/b.ts ok\n' >"$root/client/baselines/consistency.baseline"
printf 'two words extra\n' >"$root/client/baselines/mock-ratchet.baseline"
run "$root"
check 1 "a malformed mock line exits 1"
expect_out "violations: client/baselines/mock-ratchet.baseline:1 does not match"

rm "$root/client/baselines/module-shape.baseline"
printf 'a b\n' >"$root/client/baselines/mock-ratchet.baseline"
run "$root"
check 1 "a missing baseline exits 1"
expect_out "client/baselines/module-shape.baseline"

run "$repo"
check 0 "the real repo lists cleanly"
want=$(cd "$repo" && cat client/baselines/consistency.baseline client/baselines/module-shape.baseline client/baselines/mock-ratchet.baseline services/go-api/module-shape.baseline | grep -c .)
got=$(grep -c . <<<"$OUT")
[ "$got" = "$want" ] || { echo "FAIL real repo: want $want lines, got $got"; fail=1; }

[ $fail = 0 ] && echo "violations.test.sh: all cases pass"
exit $fail
