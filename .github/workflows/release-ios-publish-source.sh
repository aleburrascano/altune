#!/usr/bin/env bash

set -euo pipefail

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git worktree add wt altstore
cp apps.json wt/apps.json
cd wt
if git diff --quiet -- apps.json; then echo "apps.json unchanged"; exit 0; fi
git add apps.json
git commit -m "ci(release): altstore source v${VERSION}"
git push origin altstore
