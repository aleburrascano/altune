#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
dist=$root/apps/mobile/dist
entry=$(grep -o '/_expo/static/js/web/entry-[0-9a-f]*\.js' "$dist/index.html" | head -1)
[ -n "$entry" ] || { echo "::error::dist/index.html names no entry bundle"; exit 1; }
echo "entry=$entry" >> "$GITHUB_OUTPUT"
mkdir -p "$root/web-release"
tar -czf "$root/web-release/web.tgz" -C "$dist" .
cp "$root/services/go-api/deploy/web-release.sh" "$root/web-release/"
