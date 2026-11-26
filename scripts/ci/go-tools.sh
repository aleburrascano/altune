#!/usr/bin/env bash

set -euo pipefail

bin=$(go env GOPATH)/bin

curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v2.13.2/install.sh | sh -s -- -b "$bin" v2.13.2
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0

if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$bin" >> "$GITHUB_PATH"
fi
