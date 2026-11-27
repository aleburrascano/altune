#!/usr/bin/env bash

set -euo pipefail

golangci_version=v2.13.2
govulncheck_version=v1.8.0

bin=$(go env GOPATH)/bin

curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/${golangci_version}/install.sh" | sh -s -- -b "$bin" "$golangci_version"
go install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"
go install github.com/fe3dback/go-arch-lint@v1.19.1

if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$bin" >> "$GITHUB_PATH"
fi
