#!/usr/bin/env bash

set -euo pipefail

APP=${1:?usage: release-ios-sign.sh <path to .app>}
IOS_DIR="$GITHUB_WORKSPACE/client/ios"
APP_ENTITLEMENTS="$IOS_DIR/Altune/Altune.entitlements"
WIDGET_ENTITLEMENTS="$IOS_DIR/.targets/AltuneWidget/generated.entitlements"
APP_GROUP_KEY="com.apple.security.application-groups"

for file in "$APP_ENTITLEMENTS" "$WIDGET_ENTITLEMENTS"; do
  [ -f "$file" ] || { echo "::error::missing entitlements file $file"; exit 1; }
done

adhoc_sign() {
  codesign --force --sign - --timestamp=none --generate-entitlement-der "$@"
}

assert_app_group() {
  local entitlements
  entitlements=$(codesign -d --entitlements - --xml "$1" 2>/dev/null) || entitlements=""
  [[ "$entitlements" == *"$APP_GROUP_KEY"* ]] \
    || { echo "::error::$1 carries no App Group entitlement"; exit 1; }
}

find "$APP" \( -name '*.framework' -o -name '*.dylib' \) -prune -print0 \
  | while IFS= read -r -d '' nested; do adhoc_sign "$nested"; done

WIDGET="$APP/PlugIns/AltuneWidget.appex"
[ -d "$WIDGET" ] || { echo "::error::no AltuneWidget.appex in $APP"; exit 1; }
adhoc_sign --entitlements "$WIDGET_ENTITLEMENTS" "$WIDGET"
adhoc_sign --entitlements "$APP_ENTITLEMENTS" "$APP"

assert_app_group "$WIDGET"
assert_app_group "$APP"
echo "Signed $APP and its widget with the App Group entitlement"
