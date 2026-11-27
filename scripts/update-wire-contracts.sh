#!/usr/bin/env bash
set -euo pipefail

root=$(git rev-parse --show-toplevel) || { echo "update-wire-contracts: not inside a git checkout" >&2; exit 2; }
cd "$root/services/go-api"
export GOFLAGS=-mod=readonly

mapfile -t pkgs < <(grep -rl --include='*_test.go' '"altune/go-api/internal/shared/jsoncontract"' internal | xargs -n1 dirname | sort -u | grep -v '^internal/shared/jsoncontract$' | sed 's#^#./#')
if [ "${#pkgs[@]}" -eq 0 ]; then
  echo "update-wire-contracts: no package imports jsoncontract, so there is nothing to update" >&2
  exit 1
fi

go test -count=1 -run 'TestWire_' "${pkgs[@]}" -update-contract
echo "update-wire-contracts: rewrote contracts/wire from ${#pkgs[@]} packages; review it with: git diff contracts/wire"
