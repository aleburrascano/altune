#!/usr/bin/env bash

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FAILURES=0
SHA_A=aaaaaaa1
SHA_B=bbbbbbb2
SHA_C=ccccccc3

fresh_root() {
    WORK=$(mktemp -d)
    mkdir -p "$WORK/bin" "$WORK/tarballs"
    : >"$WORK/links.log"
    for tool in ln mv; do
        cat >"$WORK/bin/$tool" <<EOF
#!/usr/bin/env bash
printf '%s %s\n' "$tool" "\$*" >> "$WORK/links.log"
exec /usr/bin/$tool "\$@"
EOF
    done
    chmod +x "$WORK/bin"/*
}

export_tarball() {
    local sha=$1 src="$WORK/export-$1"
    mkdir -p "$src/_expo"
    printf '<html>%s</html>\n' "$sha" >"$src/index.html"
    printf 'bundle %s\n' "$sha" >"$src/_expo/entry.js"
    tar -czf "$WORK/tarballs/$sha.tgz" -C "$src" .
    printf '%s' "$WORK/tarballs/$sha.tgz"
}

release() {
    (PATH="$WORK/bin:$PATH" WEB_ROOT="$WORK/web" LOCK_TIMEOUT=2 \
        bash "$HERE/web-release.sh" "$@" >"$WORK/out.log" 2>&1)
    RC=$?
}

served() {
    cat "$WORK/web/staging/current/index.html" 2>/dev/null
}

releases_on_disk() {
    find "$WORK/web/staging/releases" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort | tr '\n' ' '
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_served() {
    [ "$(served)" = "<html>$1</html>" ] || fail "expected current to serve $1, got '$(served)'"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

CASE="a first release unpacks and points current at it with a relative link"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
expect_rc 0
expect_served "$SHA_A"
[ "$(readlink "$WORK/web/staging/current")" = "releases/$SHA_A" ] ||
    fail "current is not the relative link releases/$SHA_A"
[ -f "$WORK/web/staging/current/_expo/entry.js" ] || fail "nested bundle files were not unpacked"

CASE="an unpacked release is readable by a Caddy that does not own it"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
[ "$(stat -c '%a' "$WORK/web/staging/releases/$SHA_A")" = 755 ] ||
    fail "release dir mode is $(stat -c '%a' "$WORK/web/staging/releases/$SHA_A"), not 755"

CASE="the next release clears an unpack dir a killed release left behind"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
mkdir -p "$WORK/web/staging/releases/.unpack-$SHA_B.crashed"
release staging "$SHA_C" "$(export_tarball "$SHA_C")"
expect_rc 0
[ "$(releases_on_disk)" = "$SHA_A $SHA_C " ] || fail "expected only finished releases, got $(releases_on_disk)"

CASE="a new release flips current by renaming a fresh link over it, never rewriting it in place"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
: >"$WORK/links.log"
release staging "$SHA_B" "$(export_tarball "$SHA_B")"
expect_rc 0
expect_served "$SHA_B"
grep -qE '^ln .*/current$' "$WORK/links.log" && fail "ln wrote the live current link in place"
grep -qE '^mv -T .*/\.current-[^ ]+ .*/staging/current$' "$WORK/links.log" ||
    fail "current was not replaced by an atomic rename (mv -T)"
[ -z "$(find "$WORK/web/staging" -maxdepth 1 -name '.current-*')" ] || fail "a temp link was left behind"

CASE="rerunning with a previous sha rolls back without needing its tarball"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
release staging "$SHA_B" "$(export_tarball "$SHA_B")"
release staging "$SHA_A"
expect_rc 0
expect_served "$SHA_A"

CASE="rerunning the live sha is a no-op flip"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
expect_rc 0
expect_served "$SHA_A"
[ "$(releases_on_disk)" = "$SHA_A " ] || fail "expected only $SHA_A on disk, got $(releases_on_disk)"

CASE="a corrupt tarball fails and leaves current and the release list untouched"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
printf 'not a tarball' >"$WORK/tarballs/corrupt.tgz"
release staging "$SHA_B" "$WORK/tarballs/corrupt.tgz"
expect_rc 1
expect_out "current is untouched"
expect_served "$SHA_A"
[ "$(releases_on_disk)" = "$SHA_A " ] || fail "a failed unpack left $(releases_on_disk)"

CASE="an export with no index.html at its root is refused before the flip"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
mkdir -p "$WORK/nested/dist"
printf '<html>nested</html>\n' >"$WORK/nested/dist/index.html"
tar -czf "$WORK/tarballs/nested.tgz" -C "$WORK/nested" .
release staging "$SHA_B" "$WORK/tarballs/nested.tgz"
expect_rc 1
expect_out "no index.html"
expect_served "$SHA_A"
[ "$(releases_on_disk)" = "$SHA_A " ] || fail "a refused export left $(releases_on_disk)"

CASE="an unknown sha with no tarball fails and leaves current untouched"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
release staging "$SHA_B"
expect_rc 1
expect_served "$SHA_A"

CASE="only the newest five releases are kept, and the live one is always among them"
fresh_root
for n in 1 2 3 4 5 6 7; do
    release staging "abcdef$n" "$(export_tarball "abcdef$n")"
done
release staging abcdef3
expect_rc 0
expect_served abcdef3
[ "$(releases_on_disk)" = "abcdef3 abcdef4 abcdef5 abcdef6 abcdef7 " ] ||
    fail "expected abcdef3..7 kept, got $(releases_on_disk)"
release staging "$SHA_C" "$(export_tarball "$SHA_C")"
[ "$(releases_on_disk)" = "abcdef3 abcdef5 abcdef6 abcdef7 $SHA_C " ] ||
    fail "expected the rolled-back abcdef3 to outlive abcdef4, got $(releases_on_disk)"

CASE="a tier other than staging or prod is refused before anything is written"
fresh_root
release preview "$SHA_A" "$(export_tarball "$SHA_A")"
expect_rc 1
expect_out "unknown tier"
[ ! -e "$WORK/web" ] || fail "a refused tier created $WORK/web"

CASE="a sha that is not a hex commit cannot steer the release path"
fresh_root
release staging "../../escape" "$(export_tarball "$SHA_A")"
expect_rc 1
expect_out "not a 7-40 char lowercase hex commit"
[ -e "$WORK/web" ] && fail "a refused sha created $WORK/web"
[ -e "$WORK/escape" ] && fail "a refused sha escaped the web root"

CASE="a hex-looking sha with path separators is refused"
fresh_root
release staging "../aaaaaaa1" "$(export_tarball "$SHA_A")"
expect_rc 1
[ -e "$WORK/web" ] && fail "a refused sha created $WORK/web"

CASE="a sha shorter than an abbreviated commit is refused"
fresh_root
release staging abc "$(export_tarball "$SHA_A")"
expect_rc 1
expect_out "not a 7-40 char lowercase hex commit"

CASE="a call without a sha prints the usage instead of guessing"
fresh_root
release staging
expect_rc 1
expect_out "usage: web-release.sh"

CASE="a second release waits on the first and gives up when the lock stays held"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
exec 8>"$WORK/web/staging/.release.lock"
flock 8
release staging "$SHA_B" "$(export_tarball "$SHA_B")"
exec 8>&-
expect_rc 1
expect_out "held the lock"
expect_served "$SHA_A"

CASE="prod releases live beside staging without touching it"
fresh_root
release staging "$SHA_A" "$(export_tarball "$SHA_A")"
release prod "$SHA_B" "$(export_tarball "$SHA_B")"
expect_rc 0
expect_served "$SHA_A"
[ "$(readlink "$WORK/web/prod/current")" = "releases/$SHA_B" ] || fail "prod current is not $SHA_B"

CASE="a stale release that cannot be pruned still leaves the flipped release live and exits 0"
fresh_root
for n in 1 2 3 4 5; do
    release staging "abcdef$n" "$(export_tarball "abcdef$n")"
done
cat >"$WORK/bin/rm" <<EOF
#!/usr/bin/env bash
case "\$*" in *abcdef1*) exit 1 ;; esac
exec /usr/bin/rm "\$@"
EOF
chmod +x "$WORK/bin/rm"
mkdir -p "$WORK/web/staging/releases/.unpack-abcdef1.crashed"
release staging "$SHA_C" "$(export_tarball "$SHA_C")"
expect_rc 0
expect_served "$SHA_C"
expect_out "could not prune abcdef1"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all web release checks passed\n'
