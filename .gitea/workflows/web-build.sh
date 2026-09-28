#!/usr/bin/env bash
set -euo pipefail
cd "$WT/apps/mobile"
rm -rf dist "$WT/web-release"
npx expo export -p web
bash "$WT/.github/workflows/deploy-web-csp-check.sh"
bash "$WT/.github/workflows/deploy-web-pack.sh"
