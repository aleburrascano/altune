#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
FAILURES=0
fail() { printf 'FAIL: %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

mkdir -p "$WORK/bin" "$WORK/gopath/bin"
cat >"$WORK/bin/go" <<STUB
#!/usr/bin/env bash
if [ "\$1" = env ] && [ "\$2" = GOPATH ]; then echo "$WORK/gopath"; exit 0; fi
printf 'go %s toolchain=%s\n' "\$*" "\${GOTOOLCHAIN:-}" >>"$WORK/calls.log"
STUB
cat >"$WORK/bin/govulncheck" <<STUB
#!/usr/bin/env bash
printf 'govulncheck %s toolchain=%s\n' "\$*" "\${GOTOOLCHAIN:-}" >>"$WORK/calls.log"
exit "\${VULN_RC:-0}"
STUB
chmod +x "$WORK/bin/go" "$WORK/bin/govulncheck"

want="go$(awk '/^go /{print $2; exit}' "$HERE/../go.mod")"

: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$HERE/guardrails.sh" vuln >"$WORK/out.log" 2>&1
grep -qxF "govulncheck ./... toolchain=$want" "$WORK/calls.log" || fail "govulncheck must run under the toolchain go.mod names ($want), not whatever go is installed: $(cat "$WORK/calls.log")"

: >"$WORK/calls.log"
GOTOOLCHAIN=go1.99.0 PATH="$WORK/bin:/usr/bin:/bin" bash "$HERE/guardrails.sh" vuln >"$WORK/out.log" 2>&1
grep -qxF "govulncheck ./... toolchain=go1.99.0" "$WORK/calls.log" || fail "a GOTOOLCHAIN the caller sets is kept: $(cat "$WORK/calls.log")"

rm "$WORK/bin/govulncheck"
: >"$WORK/calls.log"
env -u GOTOOLCHAIN PATH="$WORK/bin:/usr/bin:/bin" bash "$HERE/guardrails.sh" vuln >"$WORK/out.log" 2>&1
grep -qF "go install golang.org/x/vuln/cmd/govulncheck@v1.8.0" "$WORK/calls.log" || fail "a missing govulncheck is installed at the version scripts/ci/go-tools.sh pins: $(cat "$WORK/calls.log")"
grep -qF "govulncheck@latest" "$WORK/calls.log" && fail "govulncheck must not be installed at latest"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'guardrails.sh: all cases pass\n'
