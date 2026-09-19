#!/usr/bin/env bash
# Go API guardrails — local parity with the CI `static` job.
# Usage (from anywhere):  bash services/go-api/scripts/guardrails.sh [target]
#   all         (default) lint + strict + vuln + nilaway
#   lint        depguard import direction + inward layering (blocking, all code)
#   strict      full strict-linter backlog (errcheck/staticcheck/gocritic/...)
#   vuln        govulncheck reachable-CVE scan
#   nilaway     nil-panic ceiling ratchet vs nilaway-baseline.txt
#   fmt         rewrite ONLY changed Go files with the gate's OWN formatter
#   fmt-all     rewrite BOTH whole modules (intentional full-tree sweep)
# Tool versions are pinned to match CI.
#
# fmt is the formatter of record. It runs golangci-lint's BUNDLED gofumpt via
# --config .golangci.strict.yml — the exact formatter the gate enforces. Do NOT
# run standalone `gofumpt -w`: it splits stdlib from local imports, while the
# bundled formatter wants a single alphabetical group (local `altune/...` first).
# The two disagree, and the gate follows the bundled one, so plain gofumpt
# produces files CI rejects (this bounced PR #1480). Unlike the read-only check
# targets above, fmt mutates files, so it is not part of `all`.
#
# fmt scopes to the .go files changed vs the merge base with origin/main (both
# committed and uncommitted). main is not in the bundled style, so a full-tree
# sweep dirties hundreds of unrelated files (262 and 255 in two prior PRs); the
# scope keeps a change's diff to the change. fmt-all runs the whole tree for the
# rare deliberate sweep. Both cover go-api and overseer (they share the config).
set -uo pipefail

NILAWAY_VERSION="571480214735"
GOLANGCI_VERSION="v2.12.2"
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"

cd "$(dirname "$0")/.." || exit 1   # services/go-api
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
  golangci-lint run --config .golangci.strict.yml ./... || true   # advisory locally
}

do_fmt() {
  echo "== gofumpt via bundled formatter (only .go files changed vs origin/main) =="
  have golangci-lint || go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"
  local root base f goapi=() overseer=()
  root="$(git rev-parse --show-toplevel)"
  base="$(git merge-base origin/main HEAD)"
  while IFS= read -r f; do
    [ -f "${root}/${f}" ] || continue   # skip deletes/renames-away
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
  # The count is platform-specific (build-tagged files differ per GOOS), so the
  # ceiling in nilaway-baseline.txt is the CI Linux/amd64 number. On Windows the
  # local count is lower, so this passes locally; CI is the authoritative gate.
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
