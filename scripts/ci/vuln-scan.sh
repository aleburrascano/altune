#!/usr/bin/env bash
set -euo pipefail
usage="usage: vuln-scan.sh [--osv|--govuln]   run anywhere in the repo; fails when govulncheck finds a reachable vulnerability in go-api or overseer, or osv-scanner finds a known vulnerability in a Go module file or a JavaScript lockfile; with no flag runs both, and exits 2 when a scanner could not run"
mode=${1:-all}
case "$mode" in all | --osv | --govuln) ;; *) echo "$usage" >&2; exit 2 ;; esac
[ $# -le 1 ] || { echo "$usage" >&2; exit 2; }
govulncheck_version=v1.8.0
osv_version=v2.7.1
root=$(git rev-parse --show-toplevel)
cd "$root"
failed=0
broken=0
tools=${VULN_SCAN_TOOLS:-${XDG_CACHE_HOME:-$HOME/.cache}/altune-vuln-scan}

tool() {
  local name=$1 package=$2 version=$3 dir="$tools/$3"
  if [ ! -x "$dir/$name" ]; then
    mkdir -p "$dir"
    GOTOOLCHAIN=auto GOBIN="$dir" go install "$package@$version" >&2 || return 1
  fi
  printf '%s\n' "$dir/$name"
}

scan_modules() {
  local module rc bin
  bin=$(tool govulncheck golang.org/x/vuln/cmd/govulncheck "$govulncheck_version") || {
    echo "vuln-scan: could not build govulncheck $govulncheck_version, so the Go modules were not checked" >&2
    broken=1
    return
  }
  for module in services/go-api services/overseer; do
    rc=0
    (cd "$module" && "$bin" ./...) || rc=$?
    case $rc in
      0) ;;
      3) echo "vuln-scan: govulncheck found a reachable vulnerability in $module" >&2; failed=1 ;;
      *) echo "vuln-scan: govulncheck could not run in $module (exit $rc), so it was not checked" >&2; broken=1 ;;
    esac
  done
}

scan_lockfiles() {
  local attempt rc=0 bin
  bin=$(tool osv-scanner github.com/google/osv-scanner/v2/cmd/osv-scanner "$osv_version") || {
    echo "vuln-scan: could not build osv-scanner $osv_version, so the lockfiles were not checked" >&2
    broken=1
    return
  }
  for attempt in 1 2 3; do
    rc=0
    "$bin" scan source \
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
    1) echo "vuln-scan: osv-scanner found a known vulnerability in the files above; upgrade the package, there is no ignore list" >&2; failed=1 ;;
    *) echo "vuln-scan: osv-scanner could not run (exit $rc), so the lockfiles were not checked" >&2; broken=1 ;;
  esac
}

[ "$mode" = --osv ] || scan_modules
[ "$mode" = --govuln ] || scan_lockfiles
[ "$broken" = 0 ] || exit 2
exit "$failed"
