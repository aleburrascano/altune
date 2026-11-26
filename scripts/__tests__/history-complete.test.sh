#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1"
    FAILURES=$((FAILURES + 1))
}

g() { git -C "$WORK/repo" -c user.email=t@t -c user.name=t "$@"; }

git init -qb main "$WORK/repo"
g commit -q --allow-empty -m root
g commit -q --allow-empty -m second
g checkout -qb other HEAD~1
g commit -q --allow-empty -m "only on another branch"
side=$(g rev-parse HEAD)
g checkout -q main
g commit -q --allow-empty -m third

run() { (cd "$1" && bash "$HERE/history-complete.sh" 2>&1); }

OUT=$(run "$WORK/repo"); CODE=$?
[ "$CODE" = 0 ] || fail "a full clone should pass, got $CODE: $OUT"

git clone -q --depth 1 "file://$WORK/repo" "$WORK/shallow"
OUT=$(run "$WORK/shallow"); CODE=$?
[ "$CODE" = 1 ] || fail "a depth-1 clone should exit 1, got $CODE: $OUT"
grep -q "git fetch --unshallow" <<<"$OUT" || fail "the message should say how to fix it: $OUT"

git clone -q --no-local "$WORK/repo" "$WORK/elsewhere"
printf '%s\n' "$side" >"$WORK/elsewhere/.git/shallow"
OUT=$(run "$WORK/elsewhere"); CODE=$?
[ "$CODE" = 0 ] || fail "a shallow entry outside HEAD's history should pass, got $CODE: $OUT"

OUT=$(cd "$WORK/repo" && bash "$HERE/history-complete.sh" extra 2>&1); CODE=$?
[ "$CODE" = 2 ] || fail "an argument should exit 2, got $CODE: $OUT"

[ "$FAILURES" = 0 ] && echo "history-complete.test.sh: all cases pass"
exit "$FAILURES"
