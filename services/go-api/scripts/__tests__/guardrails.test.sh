#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
SCRIPT=$HERE/../guardrails.sh
GOMOD=$HERE/../../go.mod
TOOLS=$HERE/../../../../scripts/ci/go-tools.sh
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
FAILURES=0
fail() { printf 'FAIL: %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

PINNED=$(sed -n 's/^govulncheck_version=//p' "$TOOLS" | head -n 1)
[ -n "$PINNED" ] || { echo "FAIL: scripts/ci/go-tools.sh sets no govulncheck_version"; exit 1; }
WANT_TOOLCHAIN="go$(awk '/^go /{print $2; exit}' "$GOMOD")"

mkdir -p "$WORK/bin" "$WORK/gopath/bin"
cat >"$WORK/bin/go" <<STUB
#!/usr/bin/env bash
if [ "\$1" = env ] && [ "\$2" = GOPATH ]; then echo "$WORK/gopath"; exit 0; fi
printf 'go %s toolchain=%s\n' "\$*" "\${GOTOOLCHAIN:-}" >>"$WORK/calls.log"
STUB
install_govulncheck_stub() {
    cat >"$WORK/bin/govulncheck" <<STUB
#!/usr/bin/env bash
if [ "\$1" = -version ]; then echo "Scanner: govulncheck@\${STUB_VERSION:-$PINNED}"; exit 0; fi
printf 'govulncheck %s toolchain=%s\n' "\$*" "\${GOTOOLCHAIN:-}" >>"$WORK/calls.log"
exit "\${VULN_RC:-0}"
STUB
    chmod +x "$WORK/bin/govulncheck"
}
chmod +x "$WORK/bin/go"
install_govulncheck_stub
run() { PATH="$WORK/bin:/usr/bin:/bin" bash "$SCRIPT" "$@" >"$WORK/out.log" 2>&1; RC=$?; }

: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$SCRIPT" vuln >"$WORK/out.log" 2>&1
grep -qxF "govulncheck ./... toolchain=$WANT_TOOLCHAIN" "$WORK/calls.log" || fail "govulncheck must run under the toolchain go.mod names ($WANT_TOOLCHAIN), not whatever go is installed: $(cat "$WORK/calls.log")"
grep -q "go install" "$WORK/calls.log" && fail "an installed govulncheck at the pinned version is not installed again: $(cat "$WORK/calls.log")"

: >"$WORK/calls.log"
GOTOOLCHAIN=go1.99.0 run vuln
grep -qxF "govulncheck ./... toolchain=go1.99.0" "$WORK/calls.log" || fail "a GOTOOLCHAIN the caller sets is kept: $(cat "$WORK/calls.log")"

: >"$WORK/calls.log"
STUB_VERSION=v0.0.1 env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$SCRIPT" vuln >"$WORK/out.log" 2>&1
grep -qxF "go install golang.org/x/vuln/cmd/govulncheck@$PINNED toolchain=$WANT_TOOLCHAIN" "$WORK/calls.log" || fail "an installed govulncheck at another version is replaced by the one scripts/ci/go-tools.sh pins ($PINNED): $(cat "$WORK/calls.log")"

rm "$WORK/bin/govulncheck"
: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$SCRIPT" vuln >"$WORK/out.log" 2>&1
grep -qxF "go install golang.org/x/vuln/cmd/govulncheck@$PINNED toolchain=$WANT_TOOLCHAIN" "$WORK/calls.log" || fail "a missing govulncheck is installed at the version scripts/ci/go-tools.sh pins ($PINNED): $(cat "$WORK/calls.log")"
install_govulncheck_stub

if grep -qE '^(GOVULNCHECK|GOLANGCI)_VERSION="?v[0-9]' "$SCRIPT"; then
    fail "guardrails.sh must read tool versions from scripts/ci/go-tools.sh, not state them"
fi

mkdir -p "$WORK/repo/services/go-api/scripts" "$WORK/repo/scripts/ci"
cp "$SCRIPT" "$WORK/repo/services/go-api/scripts/guardrails.sh"
printf 'golangci_version=v9.9.9\n' >"$WORK/repo/scripts/ci/go-tools.sh"
printf 'module x\n\ngo 1.26.11\n' >"$WORK/repo/services/go-api/go.mod"
: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$WORK/repo/services/go-api/scripts/guardrails.sh" vuln >"$WORK/out.log" 2>&1
RC=$?
[ "$RC" = 2 ] && grep -qF "govulncheck_version are not set" "$WORK/out.log" || fail "a go-tools.sh with no pinned version fails with a clear message and exit 2 (exit $RC): $(cat "$WORK/out.log")"

printf 'golangci_version=v9.9.9\ngovulncheck_version=v9.9.9\n' >"$WORK/repo/scripts/ci/go-tools.sh"
printf 'module x\n' >"$WORK/repo/services/go-api/go.mod"
: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$WORK/repo/services/go-api/scripts/guardrails.sh" vuln >"$WORK/out.log" 2>&1
RC=$?
[ "$RC" = 2 ] && grep -qF "has no go line" "$WORK/out.log" || fail "a go.mod with no go line fails with a clear message and exit 2 (exit $RC): $(cat "$WORK/out.log")"
[ ! -s "$WORK/calls.log" ] || fail "nothing runs when there is no toolchain to pin: $(cat "$WORK/calls.log")"

: >"$WORK/calls.log"
GOTOOLCHAIN=go1.26.11 PATH="$WORK/bin:/usr/bin:/bin" bash "$WORK/repo/services/go-api/scripts/guardrails.sh" vuln >"$WORK/out.log" 2>&1
RC=$?
[ "$RC" = 0 ] || fail "a caller-set GOTOOLCHAIN needs no go line (exit $RC): $(cat "$WORK/out.log")"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'guardrails.sh: all cases pass\n'
