#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/vuln-scan.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/scripts/ci" "$work/services/go-api" "$work/services/overseer" "$work/bin"
cp "$script" "$work/scripts/ci/"
git -C "$work" init -q
cat >"$work/bin/go" <<'SH'
#!/usr/bin/env bash
printf 'go %s toolchain=%s\n' "$*" "${GOTOOLCHAIN:-}" >>"$STUB_LOG"
[ "$1" = install ] || exit 99
[ "${STUB_INSTALL_RC:-0}" = 0 ] || exit "$STUB_INSTALL_RC"
name=$(basename "${2%@*}")
cat >"$GOBIN/$name" <<TOOL
#!/usr/bin/env bash
printf '%s %s\n' "\$(pwd -P | sed "s#^\$WORK##")" "$name \$*" >>"\$STUB_LOG"
case "$name" in
  govulncheck) exit "\${STUB_GOVULN_RC:-0}" ;;
  osv-scanner) exit "\${STUB_OSV_RC:-0}" ;;
esac
TOOL
chmod +x "$GOBIN/$name"
SH
chmod +x "$work/bin/go"

run() {
  rm -rf "$work/tools"
  (cd "$work/services" && PATH="$work/bin:$PATH" VULN_SCAN_TOOLS="$work/tools" WORK="$(cd "$work" && pwd -P)" STUB_LOG="$work/go.log" "$@" bash "$work/scripts/ci/vuln-scan.sh" "${ARGS[@]}" 2>&1)
}
t() {
  local want=$1 name=$2 contains=$3; shift 3
  local out rc=0
  : >"$work/go.log"
  out=$(run env "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" <<<"$out"; then
    echo "FAIL $name: exit $rc (want $want), output: $out"
    fail=1
  fi
}
logged() {
  grep -qF -- "$1" "$work/go.log" || { echo "FAIL $2: go.log lacks '$1': $(cat "$work/go.log")"; fail=1; }
}
not_logged() {
  ! grep -qF -- "$1" "$work/go.log" || { echo "FAIL $2: go.log has '$1': $(cat "$work/go.log")"; fail=1; }
}

ARGS=()
t 0 "clean scans pass" ""
not_logged "--config" "no ignore list is passed to osv-scanner"
logged "/services/go-api govulncheck ./..." "go-api is scanned from its own module"
logged "/services/overseer govulncheck ./..." "overseer is scanned from its own module"
logged "--lockfile pnpm-lock.yaml" "the pnpm lockfile is scanned"
logged "--lockfile services/go-api/go.mod" "the go-api module file is scanned"
logged "--lockfile services/overseer/web/package-lock.json" "the overseer web lockfile is scanned"

t 0 "tools build with their own toolchain when the job pins one" "" GOTOOLCHAIN=go1.26.11
logged "go install golang.org/x/vuln/cmd/govulncheck@v1.8.0 toolchain=auto" "govulncheck builds under GOTOOLCHAIN=auto"
logged "go install github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.7.1 toolchain=auto" "osv-scanner builds under GOTOOLCHAIN=auto"

t 1 "a reachable vulnerability fails the run" "found a reachable vulnerability in services/go-api" STUB_GOVULN_RC=3
t 2 "a govulncheck that cannot run exits 2" "govulncheck could not run in services/go-api (exit 2)" STUB_GOVULN_RC=2
t 1 "a vulnerable lockfile fails the run" "osv-scanner found a known vulnerability" STUB_OSV_RC=1
t 2 "osv-scanner failing on the network exits 2 after three attempts" "osv-scanner could not run (exit 127)" STUB_OSV_RC=127
[ "$(grep -c ' osv-scanner scan ' "$work/go.log")" = 3 ] || { echo "FAIL osv-scanner was not tried three times: $(cat "$work/go.log")"; fail=1; }
t 2 "a tool that cannot be built exits 2" "could not build govulncheck" STUB_INSTALL_RC=1

ARGS=(--osv)
t 0 "--osv skips govulncheck" ""
not_logged govulncheck "--osv skips govulncheck"
ARGS=(--govuln)
t 0 "--govuln skips osv-scanner" ""
not_logged osv-scanner "--govuln skips osv-scanner"
ARGS=(--nonsense)
t 2 "an unknown flag is refused" "usage: vuln-scan.sh"

[ $fail = 0 ] && echo "vuln-scan.test.sh: ok"
exit $fail
