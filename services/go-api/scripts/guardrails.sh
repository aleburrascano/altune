#!/usr/bin/env bash
set -uo pipefail

NILAWAY_VERSION="571480214735"
GOLANGCI_VERSION="v2.12.2"
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"

cd "$(dirname "$0")/.." || exit 1
GOBIN="$(go env GOPATH)/bin"
export PATH="$PATH:$GOBIN"

target="${1:-all}"
rc=0

have() { command -v "$1" >/dev/null 2>&1; }

do_lint() {
  echo "== depguard: import direction + inward layering (blocking) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  golangci-lint run ./... || rc=1
}

do_strict() {
  echo "== strict linters (full backlog; CI gates only NEW findings) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  golangci-lint run --config .golangci.strict.yml ./... || true
}

do_fmt() {
  echo "== gofumpt via bundled formatter (only .go files changed vs origin/main) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  local root base f goapi=() overseer=()
  root="$(git rev-parse --show-toplevel)"
  base="$(git merge-base origin/main HEAD)"
  while IFS= read -r f; do
    [ -f "${root}/${f}" ] || continue
    case "${f}" in
      services/go-api/*)   goapi+=("${f#services/go-api/}") ;;
      services/overseer/*) overseer+=("${f#services/overseer/}") ;;
    esac
  done < <(git -C "${root}" diff --name-only "${base}" -- '*.go')
  if [ "${#goapi[@]}" -eq 0 ] && [ "${#overseer[@]}" -eq 0 ]; then
    echo "no changed .go files vs origin/main; nothing to format"
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
  have govulncheck || go install golang.org/x/vuln/cmd/govulncheck@latest
  govulncheck ./... || rc=1
}

do_nilaway() {
  echo "== nilaway ceiling ratchet =="
  have nilaway || go install "go.uber.org/nilaway/cmd/nilaway@${NILAWAY_VERSION}"
  local base count
  base="$(tr -dc '0-9' < nilaway-baseline.txt)"
  count="$(nilaway ./... 2>&1 | sed 's/\x1b\[[0-9;]*m//g' | grep -c 'Potential nil panic detected')"
  echo "nilaway findings: ${count} (ceiling ${base})"
  if [ "${count}" -gt "${base}" ]; then
    echo "ERROR: nilaway findings rose from ${base} to ${count}"
    rc=1
  fi
}

case "${target}" in
  lint)    do_lint ;;
  strict)  do_strict ;;
  fmt)     do_fmt ;;
  fmt-all) do_fmt_all ;;
  vuln)    do_vuln ;;
  nilaway) do_nilaway ;;
  all)     do_lint; do_strict; do_vuln; do_nilaway ;;
  *)       echo "unknown target: ${target}"; exit 2 ;;
esac

[ "${rc}" -eq 0 ] && echo "guardrails: OK" || echo "guardrails: FAILED"
exit "${rc}"
