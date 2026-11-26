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

mkdir -p "$WORK/repo/scripts" "$WORK/repo/svc/a" "$WORK/repo/other/b"
cp "$HERE/test-home-tree.sh" "$HERE/test-home.mjs" "$WORK/repo/scripts/"
cd "$WORK/repo" || exit 1
git init -q
git config user.email t@t
git config user.name t
git commit -q --allow-empty -m root
touch svc/a/unit.go svc/a/unit_test.go svc/a/unit_extra_test.go other/b/thing.go other/b/thing_test.go other/b/thing_extra_test.go
git add -A
git commit -qm base

OUT=$(bash scripts/test-home-tree.sh other 2>&1)
CODE=$?
[ "$CODE" = 1 ] || fail "a misplaced test file under the checked directory should exit 1, got $CODE: $OUT"
grep -qF "other/b/thing_extra_test.go" <<<"$OUT" || fail "the finding should name the file: $OUT"
if grep -qF "svc/a/unit_extra_test.go" <<<"$OUT"; then fail "a finding outside the checked directory should not be reported: $OUT"; fi

git rm -q other/b/thing_extra_test.go
git commit -qm fixed
OUT=$(bash scripts/test-home-tree.sh other 2>&1)
CODE=$?
[ "$CODE" = 0 ] || fail "a directory at zero should exit 0 although another directory still has findings, got $CODE: $OUT"

[ "$FAILURES" = 0 ] && echo "test-home-tree.test.sh: all cases pass"
exit "$FAILURES"
