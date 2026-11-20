#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")/.." && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0
SECRET='.youtube.com	TRUE	/	TRUE	1999999999	LOGIN_INFO	never-print-me'
CHECK="docker run --rm --pull never --network none --entrypoint sh -v"

setup_checkout() {
    WORK=$(mktemp -d -p "$parent")
    CHECKOUT="$WORK/altune"
    mkdir -p "$WORK/bin" "$CHECKOUT/services/go-api/deploy"
    cp "$HERE/cookie-jar-dir.sh" "$CHECKOUT/services/go-api/deploy/"
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
if [ "\$1 \$2" = "image inspect" ]; then
    case " \${STUB_IMAGES-altune-go-api:blue} " in *" \$3 "*) exit 0 ;; esac
    exit 1
fi
exit \${STUB_CHECK_RC:-0}
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
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1', got: $(cat "$WORK/out.log")"
}

expect_jar_in_folder() {
    [ "$(cat "$CHECKOUT/cookies/cookies.txt" 2>/dev/null)" = "$SECRET" ] || fail "expected the jar's contents at cookies/cookies.txt"
}

expect_modes_set() {
    [ "$(stat -c %a "$CHECKOUT/cookies")" = 2770 ] || fail "expected the folder at 2770, got $(stat -c %a "$CHECKOUT/cookies")"
    [ "$(stat -c %a "$CHECKOUT/cookies/cookies.txt")" = 660 ] || fail "expected the jar at 0660, got $(stat -c %a "$CHECKOUT/cookies/cookies.txt")"
}

expect_checked_with() {
    grep -qxF "$CHECK $CHECKOUT/cookies:/jar $1 -c test -r /jar/cookies.txt && test -w /jar && test -x /jar" "$WORK/actions.log" ||
        fail "expected go-api's access checked in $1 without a pull, got: $(cat "$WORK/actions.log")"
}

expect_no_access_check() {
    grep -qF "docker run" "$WORK/actions.log" && fail "expected no access check, got: $(cat "$WORK/actions.log")"
}

expect_secret_never_printed() {
    grep -qF never-print-me "$WORK/out.log" "$WORK/actions.log" && fail "the jar's contents were printed"
}

expect_done() {
    expect_out "copy it to $CHECKOUT/cookies/cookies.txt.new and then mv it over $CHECKOUT/cookies/cookies.txt"
}

expect_not_done() {
    grep -qF "done:" "$WORK/out.log" && fail "expected no done line"
}

CASE="a jar at the old path moves into the folder and nothing is left at the old path"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_jar_in_folder
[ -e "$CHECKOUT/cookies.txt" ] || [ -L "$CHECKOUT/cookies.txt" ] && fail "expected nothing left at the old path"
expect_out "stop dropping jars there"
expect_modes_set
expect_checked_with altune-go-api:blue
expect_done
expect_secret_never_printed

CASE="a rerun after the move changes nothing and succeeds"
: >"$WORK/actions.log"
run_script
expect_rc 0
expect_jar_in_folder
expect_modes_set
expect_checked_with altune-go-api:blue

CASE="a jar already in the folder only gets its modes set and checked"
setup_checkout
mkdir -p "$CHECKOUT/cookies"
printf '%s' "$SECRET" >"$CHECKOUT/cookies/cookies.txt"
chmod 0644 "$CHECKOUT/cookies/cookies.txt"
run_script
expect_rc 0
[ -e "$CHECKOUT/cookies.txt" ] && fail "expected no file created at the old path"
expect_modes_set
expect_checked_with altune-go-api:blue

CASE="a symlink at the old path is reported plainly and left alone"
setup_checkout
mkdir -p "$CHECKOUT/cookies"
printf '%s' "$SECRET" >"$CHECKOUT/cookies/cookies.txt"
ln -s cookies/cookies.txt "$CHECKOUT/cookies.txt"
run_script
expect_rc 0
expect_out "$CHECKOUT/cookies.txt is a symlink to cookies/cookies.txt, not a jar; nothing to move"
[ "$(readlink "$CHECKOUT/cookies.txt")" = cookies/cookies.txt ] || fail "expected the symlink left in place"
expect_jar_in_folder
expect_modes_set
expect_secret_never_printed

CASE="a jar at both paths is refused rather than guessing which is newer"
setup_checkout
mkdir -p "$CHECKOUT/cookies"
printf '%s' "$SECRET" >"$CHECKOUT/cookies/cookies.txt"
printf 'other' >"$CHECKOUT/cookies.txt"
run_script
expect_rc 1
expect_out "both $CHECKOUT/cookies.txt and $CHECKOUT/cookies/cookies.txt exist"
[ "$(cat "$CHECKOUT/cookies.txt")" = other ] || fail "expected the old file left in place"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"
expect_secret_never_printed

CASE="no jar anywhere fails and says where to put one"
setup_checkout
run_script
expect_rc 1
expect_out "no cookie jar at $CHECKOUT/cookies/cookies.txt"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"

CASE="an empty folder fails without its modes being touched"
setup_checkout
mkdir -m 0755 "$CHECKOUT/cookies"
run_script
expect_rc 1
expect_out "no cookie jar at $CHECKOUT/cookies/cookies.txt"
[ "$(stat -c %a "$CHECKOUT/cookies")" = 755 ] || fail "expected the folder left at 755, got $(stat -c %a "$CHECKOUT/cookies")"
[ -s "$WORK/actions.log" ] && fail "expected no docker call"

CASE="with only staging's image on the host the check runs in it"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES=altune-staging-go-api:green run_script
expect_rc 0
expect_checked_with altune-staging-go-api:green

CASE="with no go-api image on the host the script fails instead of pulling one"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_IMAGES="" run_script
expect_rc 1
expect_out "none of altune-go-api:blue altune-go-api:green altune-staging-go-api:blue altune-staging-go-api:green is on this host"
expect_no_access_check
expect_not_done

CASE="a jar go-api's user cannot use fails the script and names the folder's owner"
setup_checkout
printf '%s' "$SECRET" >"$CHECKOUT/cookies.txt"
STUB_CHECK_RC=1 run_script
expect_rc 1
expect_out "owned by uid $(id -u) gid $(id -g), which must match go-api's uid or gid"
expect_not_done
expect_secret_never_printed

if [ "$FAILURES" -gt 0 ]; then
    printf '%d failure(s)\n' "$FAILURES"
    exit 1
fi
echo "cookie-jar-dir: all cases passed"
