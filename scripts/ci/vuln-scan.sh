#!/usr/bin/env bash
set -euo pipefail
usage="usage: vuln-scan.sh [--osv|--govuln]   run anywhere in the repo; fails when govulncheck finds a reachable vulnerability in go-api or overseer, or osv-scanner finds a known vulnerability in a Go module file or a JavaScript lockfile; with no flag runs both, and exits 2 when a scanner could not run"
mode=${1:-all}
case "$mode" in all | --osv | --govuln) ;; *) echo "$usage" >&2; exit 2 ;; esac
[ $# -le 1 ] || { echo "$usage" >&2; exit 2; }
govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.8.0
osv=github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.7.1
root=$(git rev-parse --show-toplevel)
cd "$root"
failed=0
broken=0
[ -f "$root/osv-scanner.toml" ] || { echo "vuln-scan: $root/osv-scanner.toml is missing, so the ignore list for known unfixable advisories cannot be read" >&2; exit 2; }

scan_modules() {
  local module rc
  for module in services/go-api services/overseer; do
    rc=0
    (cd "$module" && go run "$govulncheck" ./...) || rc=$?
    case $rc in
      0) ;;
      3) echo "vuln-scan: govulncheck found a reachable vulnerability in $module" >&2; failed=1 ;;
      *) echo "vuln-scan: govulncheck could not run in $module (exit $rc), so it was not checked" >&2; broken=1 ;;
    esac
  done
}

scan_lockfiles() {
  local attempt rc=0
  for attempt in 1 2 3; do
    rc=0
    go run "$osv" scan source \
      --config "$root/osv-scanner.toml" \
      --lockfile services/go-api/go.mod \
      --lockfile services/overseer/go.mod \
      --lockfile pnpm-lock.yaml \
      --lockfile client/package-lock.json \
      --lockfile services/overseer/web/package-lock.json || rc=$?
    [ "$rc" != 127 ] && break
    echo "vuln-scan: osv-scanner failed on attempt $attempt (exit 127, usually the network)" >&2
  done
  case $rc in
    0) ;;
    1) echo "vuln-scan: osv-scanner found a known vulnerability in the files above" >&2; failed=1 ;;
    *) echo "vuln-scan: osv-scanner could not run (exit $rc), so the lockfiles were not checked" >&2; broken=1 ;;
  esac
}

[ "$mode" = --osv ] || scan_modules
[ "$mode" = --govuln ] || scan_lockfiles
[ "$broken" = 0 ] || exit 2
exit "$failed"
