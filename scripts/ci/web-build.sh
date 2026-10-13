#!/usr/bin/env bash
set -euo pipefail
cd "$WT/client"
rm -rf dist "$WT/web-release"
npx expo export -p web --clear
bash "$WT/scripts/ci/deploy-web-csp-check.sh"
bash "$WT/scripts/ci/deploy-web-pack.sh"
