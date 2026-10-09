#!/usr/bin/env bash
set -euo pipefail
cd "$WT/client"
rm -rf dist "$WT/web-release"
npx expo export -p web --clear
bash "$WT/.github/workflows/deploy-web-csp-check.sh"
bash "$WT/.github/workflows/deploy-web-pack.sh"
