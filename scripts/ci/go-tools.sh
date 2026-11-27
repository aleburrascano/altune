#!/usr/bin/env bash

set -euo pipefail

golangci_version=v2.13.2
govulncheck_version=v1.8.0

bin=$(go env GOPATH)/bin

attempts=4
read -r -a backoff <<<"${GO_TOOLS_BACKOFF:-5 15 30}"

retry() {
  local label=$1 attempt rc=0
  shift
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    rc=0
    "$@" || rc=$?
    if [ "$rc" -eq 0 ]; then
      return 0
    fi
    echo "go-tools: $label failed on attempt $attempt of $attempts (exit $rc)" >&2
    if [ "$attempt" -lt "$attempts" ]; then
      echo "go-tools: retrying $label in ${backoff[attempt - 1]}s" >&2
      sleep "${backoff[attempt - 1]}"
    fi
  done
  echo "go-tools: $label failed after $attempts attempts, giving up" >&2
  return 1
}

install_golangci() {
  curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/${golangci_version}/install.sh" | sh -s -- -b "$bin" "$golangci_version"
}

retry golangci-lint install_golangci
retry govulncheck go install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"
retry go-arch-lint go install github.com/fe3dback/go-arch-lint@v1.19.1

if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$bin" >> "$GITHUB_PATH"
fi
