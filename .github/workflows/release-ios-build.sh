#!/usr/bin/env bash

set -euo pipefail

workspaces=(*.xcworkspace)
WS=${workspaces[0]}
[ -e "$WS" ] || { echo "::error::no .xcworkspace in $PWD"; exit 1; }
SCHEME=$(basename "$WS" .xcworkspace)
echo "Workspace=$WS Scheme=$SCHEME"
xcodebuild \
  -workspace "$WS" \
  -scheme "$SCHEME" \
  -configuration Release \
  -sdk iphoneos \
  -destination 'generic/platform=iOS' \
  -archivePath "$RUNNER_TEMP/Altune.xcarchive" \
  archive \
  CODE_SIGNING_ALLOWED=NO \
  CODE_SIGNING_REQUIRED=NO \
  CODE_SIGN_IDENTITY="" \
  DEVELOPMENT_TEAM=""
