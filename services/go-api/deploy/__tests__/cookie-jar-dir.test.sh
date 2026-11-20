#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0
SECRET='.youtube.com	TRUE	/	TRUE	1999999999	LOGIN_INFO	never-print-me'

setup_checkout() {
    WORK=$(mktemp -d -p "$parent")
    CHECKOUT="$WORK/altune"
    mkdir -p "$WORK/bin" "$CHECKOUT/services/go-api/deploy"
    cp "$HERE/cookie-jar-dir.sh" "$CHECKOUT/services/go-api/deploy/"
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
exit \${STUB_DOCKER_RC:-0}
EOF
    chmod +x "$WORK/bin/docker"
    : >"$WORK/actions.log"
}

run_script() {
    (cd "$CHECKOUT/services/go-api" && PATH="$WORK/bin:$PATH" bash deploy/cookie-jar-dir.sh >"$WORK/out.log" 2>&1)
    RC=$?
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

expect_jar_moved() {
    [ "$(cat "$CHECKOUT/cookies/cookies.txt" 2>/dev/null)" = "$SECRET" ] || fail "expected the jar's contents at cookies/cookies.txt"
    [ "$(readlink "$CHECKOUT/cookies.txt")" = cookies/cookies.txt ] || fail "expected a symlink from cookies.txt to cookies/cookies.txt"
}

expect_ownership_fixed() {
    grep -qF "docker run --rm -u 0 -v $CHECKOUT/cookies:/jar alpine:3.21 sh -c chown 1000 /jar /jar/cookies.txt && chmod 2775 /jar && chmod 0664 /jar/cookies.txt" "$WORK/actions.log" ||
        fail "expected the folder handed to uid 1000 with a setgid group, got: $(cat "$WORK/actions.log")"
}

expect_secret_never_printed() {
    grep -qF never-print-me "$WORK/out.log" "$WORK/actions.log" && fail "the jar's contents were printed"
}

CASE="a jar at the old path moves into the folder and the old path points at it"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_jar_moved
expect_ownership_fixed
expect_secret_never_printed

CASE="a rerun after the move changes nothing and succeeds"
: >"$WORK/actions.log"
run_script
expect_rc 0
expect_jar_moved
expect_ownership_fixed

CASE="a jar already in the folder with no old file is only re-owned"
setup_checkout
mkdir -p "$CHECKOUT/cookies"
printf '%s' "$SECRET" >"$CHECKOUT/cookies/cookies.txt"
run_script
expect_rc 0
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected no file created at the old path"
expect_ownership_fixed

CASE="a jar at both paths is refused rather than guessing which is newer"
setup_checkout
mkdir -p "$CHECKOUT/cookies"
printf '%s' "$SECRET" >"$CHECKOUT/cookies/cookies.txt"
printf 'other' >"$CHECKOUT/cookies.txt"
run_script
expect_rc 1
expect_out "both $CHECKOUT/cookies.txt and $CHECKOUT/cookies/cookies.txt exist"
[ "$(cat "$CHECKOUT/cookies.txt")" = other ] || fail "expected the old file left in place"
[ -s "$WORK/actions.log" ] && fail "expected no ownership change"
expect_secret_never_printed

CASE="no jar anywhere fails and says where to put one"
setup_checkout
run_script
expect_rc 1
expect_out "no cookie jar at $CHECKOUT/cookies/cookies.txt"
[ -s "$WORK/actions.log" ] && fail "expected no ownership change"

CASE="a failed ownership change fails the script"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_DOCKER_RC=1 run_script
expect_rc 1
expect_out "giving uid 1000 the folder"
grep -qF "done:" "$WORK/out.log" && fail "expected no done line after a failed ownership change"

if [ "$FAILURES" -gt 0 ]; then
    printf '%d failure(s)\n' "$FAILURES"
    exit 1
fi
echo "cookie-jar-dir: all cases passed"
