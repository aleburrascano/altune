#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/kit-self-tests.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
mkdir -p "$repo/scripts/ci" "$repo/scripts/__tests__"
cp "$script" "$repo/scripts/ci/"
echo 'echo ran >"$TEST_MARK"; exit "${TEST_RC:-0}"' >"$repo/scripts/__tests__/violations.test.sh"
echo 'echo v1' >"$repo/scripts/violations.mjs"
echo 'echo r1' >"$repo/README.md"
commit() { git -C "$repo" add -A && git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm "$1"; }
commit base
git -C "$repo" branch base

export TEST_MARK=$work/ran
OUT=""; CODE=0
run() { rm -f "$TEST_MARK"; OUT=$(cd "$repo" && bash scripts/ci/kit-self-tests.sh "$@" 2>&1); CODE=$?; }
expect() {
  local want=$1 ran=$2 desc=$3
  [ "$CODE" = "$want" ] || { echo "FAIL want exit $want got $CODE: $desc"; sed 's/^/    /' <<<"$OUT"; fail=1; }
  if [ "$ran" = yes ] && [ ! -e "$TEST_MARK" ]; then echo "FAIL $desc: violations.test.sh did not run"; fail=1; fi
  if [ "$ran" = no ] && [ -e "$TEST_MARK" ]; then echo "FAIL $desc: violations.test.sh ran"; fail=1; fi
}

run; expect 1 no "no base ref"
grep -q "usage: kit-self-tests.sh" <<<"$OUT" || { echo "FAIL no base ref: no usage line"; fail=1; }
run no-such-ref; expect 3 no "a base ref that does not exist"

echo 'echo r2' >"$repo/README.md"; commit "docs only"
run base; expect 0 no "violations unchanged"
grep -q "unchanged since base" <<<"$OUT" || { echo "FAIL violations unchanged: does not say so"; fail=1; }

echo 'echo v2' >"$repo/scripts/violations.mjs"; commit "violations change"
run base; expect 0 yes "violations.mjs changed"
TEST_RC=1 run base; expect 1 yes "violations.mjs changed and its test fails"

git -C "$repo" reset -q --hard base
echo '# more' >>"$repo/scripts/__tests__/violations.test.sh"; commit "test change"
run base; expect 0 yes "violations.test.sh changed"

[ "$fail" = 0 ] && echo "kit-self-tests: all passed"
exit "$fail"
