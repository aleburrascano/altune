#!/usr/bin/env bash
set -uo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
tools="${here}/../../../scripts/ci/go-tools.sh"
tool_version() { sed -n "s/^$1=//p" "${tools}" 2>/dev/null | head -n 1; }
GOLANGCI_VERSION="$(tool_version golangci_version)"
GOVULNCHECK_VERSION="$(tool_version govulncheck_version)"
if [ -z "${GOLANGCI_VERSION}" ] || [ -z "${GOVULNCHECK_VERSION}" ]; then
  echo "guardrails: golangci_version and govulncheck_version are not set in ${tools}, so there is no pinned tool version to use"
  exit 2
fi
if [ -z "${GOTOOLCHAIN:-}" ]; then
  gomod_go="$(awk '/^go /{print $2; exit}' "${here}/../go.mod" 2>/dev/null)"
  if [ -z "${gomod_go}" ]; then
    echo "guardrails: ${here}/../go.mod has no go line, so there is no toolchain to scan with; set GOTOOLCHAIN or add the line"
    exit 2
  fi
  GOTOOLCHAIN="go${gomod_go}"
fi
export GOTOOLCHAIN

cd "$(dirname "$0")/.." || exit 1
GOBIN="$(go env GOPATH)/bin"
export PATH="$PATH:$GOBIN"

target="${1:-all}"
rc=0

default_base_ref() { git rev-parse --verify --quiet gitea/main >/dev/null && echo gitea/main || echo origin/main; }

have() { command -v "$1" >/dev/null 2>&1; }

do_strict() {
  echo "== strict linters (full backlog; CI gates only NEW findings) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  golangci-lint run --config .golangci.strict.yml ./... || true
}

do_fmt() {
  echo "== gofumpt via bundled formatter (only .go files changed vs the base branch) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  local root base_ref base f goapi=() overseer=()
  root="$(git rev-parse --show-toplevel)"
  base_ref="$(default_base_ref)"
  base="$(git merge-base "${base_ref}" HEAD)"
  while IFS= read -r f; do
    [ -f "${root}/${f}" ] || continue
    case "${f}" in
      services/go-api/*)   goapi+=("${f#services/go-api/}") ;;
      services/overseer/*) overseer+=("${f#services/overseer/}") ;;
    esac
  done < <(git -C "${root}" diff --name-only "${base}" -- '*.go')
  if [ "${#goapi[@]}" -eq 0 ] && [ "${#overseer[@]}" -eq 0 ]; then
    echo "no changed .go files vs ${base_ref}; nothing to format"
    return
  fi
  [ "${#goapi[@]}" -eq 0 ] || golangci-lint fmt --config .golangci.strict.yml "${goapi[@]}" || rc=1
  [ "${#overseer[@]}" -eq 0 ] || ( cd ../overseer && golangci-lint fmt --config ../go-api/.golangci.strict.yml "${overseer[@]}" ) || rc=1
}

do_fmt_all() {
  echo "== gofumpt via bundled formatter over the WHOLE go-api module =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  golangci-lint fmt --config .golangci.strict.yml || rc=1
  echo "== same formatter over the WHOLE overseer module (shares the strict config) =="
  ( cd ../overseer && golangci-lint fmt --config ../go-api/.golangci.strict.yml ) || rc=1
}

do_vuln() {
  echo "== govulncheck: reachable CVEs (blocking) =="
  [ "$(govulncheck -version 2>/dev/null | sed -n 's/^Scanner: govulncheck@//p')" = "${GOVULNCHECK_VERSION}" ] \
    || go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
  govulncheck ./... || rc=1
}

case "${target}" in
  strict)  do_strict ;;
  fmt)     do_fmt ;;
  fmt-all) do_fmt_all ;;
  vuln)    do_vuln ;;
  all)     do_strict; do_vuln ;;
  *)       echo "unknown target: ${target}"; exit 2 ;;
esac

[ "${rc}" -eq 0 ] && echo "guardrails: OK" || echo "guardrails: FAILED"
exit "${rc}"
