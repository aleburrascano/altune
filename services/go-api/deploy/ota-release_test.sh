#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0

fresh_root() {
    WORK=$(mktemp -d -p "$parent")
    ROOT="$WORK/updates"
}

update_tarball() {
    local name=$1 src="$WORK/src-$1"
    shift
    mkdir -p "$src/assets"
    for entry in "$@"; do
        mkdir -p "$src/manifests/${entry%/*}"
        printf 'manifest %s\n' "$name" >"$src/manifests/$entry"
    done
    printf 'bundle %s\n' "$name" >"$src/assets/$name.js"
    tar -czf "$WORK/$name.tgz" -C "$src" .
    printf '%s' "$WORK/$name.tgz"
}

release() {
    (UPDATES_ROOT="$ROOT" LOCK_TIMEOUT=2 bash "$HERE/ota-release.sh" "$@" >"$WORK/out.log" 2>&1)
    RC=$?
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

CASE="a release serves its manifest and keeps the assets older manifests name"
fresh_root
release "$(update_tarball one 1.0/ios)"
expect_rc 0
release "$(update_tarball two 1.0/ios)"
expect_rc 0
[ "$(cat "$ROOT/manifests/1.0/ios" 2>/dev/null)" = 'manifest two' ] || fail "1.0/ios serves '$(cat "$ROOT/manifests/1.0/ios" 2>/dev/null)'"
[ -f "$ROOT/assets/one.js" ] || fail "the first release's asset was removed"
[ -f "$ROOT/assets/two.js" ] || fail "the second release's asset is missing"
[ -z "$(find "$ROOT" -mindepth 1 -name '.*' ! -name .release.lock)" ] || fail "temporary files left behind"

CASE="an asset already published is never rewritten"
fresh_root
release "$(update_tarball same 1.0/ios)"
printf 'served bytes\n' >"$ROOT/assets/same.js"
release "$(update_tarball same 1.0/ios)"
expect_rc 0
[ "$(cat "$ROOT/assets/same.js")" = 'served bytes' ] || fail "an existing asset was overwritten"

CASE="a tarball without exactly one manifest changes nothing"
fresh_root
release "$(update_tarball live 1.0/ios)"
release "$(update_tarball none)"
[ "$RC" != 0 ] || fail "a tarball with no manifest was accepted"
release "$(update_tarball both 1.0/ios 1.0/android)"
[ "$RC" != 0 ] || fail "a tarball with two manifests was accepted"
[ "$(cat "$ROOT/manifests/1.0/ios")" = 'manifest live' ] || fail "the live manifest changed"
[ ! -f "$ROOT/assets/none.js" ] || fail "the no-manifest tarball's assets were published"
[ ! -f "$ROOT/assets/both.js" ] || fail "the two-manifest tarball's assets were published"

CASE="a missing tarball fails loudly"
fresh_root
release "$WORK/absent.tgz"
[ "$RC" != 0 ] || fail "a missing tarball was accepted"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'all ota-release checks passed\n'
