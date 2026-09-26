#!/usr/bin/env bash

set -euo pipefail

apps=("$RUNNER_TEMP/Altune.xcarchive/Products/Applications/"*.app)
APP=${apps[0]}
[ -e "$APP" ] || { echo "::error::no .app in the archive"; exit 1; }
echo "App=$APP"
mkdir -p "$RUNNER_TEMP/Payload"
cp -R "$APP" "$RUNNER_TEMP/Payload/"
( cd "$RUNNER_TEMP" && zip -qry Altune.ipa Payload )
SIZE=$(stat -f%z "$RUNNER_TEMP/Altune.ipa")
echo "size=$SIZE" >> "$GITHUB_OUTPUT"
echo "Built Altune.ipa ($SIZE bytes)"
