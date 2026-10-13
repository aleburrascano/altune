#!/usr/bin/env bash
set -euo pipefail
body=$(mktemp)
trap 'rm -f "$body"' EXIT
status=$(curl -s --max-time 20 -o "$body" -w '%{http_code}' -H 'expo-protocol-version: 1' \
  -H "expo-runtime-version: $RUNTIME" -H 'expo-platform: ios' "$OTA_BASE_URL/updates/manifest")
[ "$status" = 200 ] || { echo "manifest for runtime $RUNTIME answered $status" >&2; exit 1; }
read -r url hash served_version < <(node -e '
  const body = require("fs").readFileSync(process.argv[1], "utf8");
  const manifest = JSON.parse(body.split("\r\n\r\n")[1].split("\r\n--altune-ota--")[0]);
  console.log(manifest.launchAsset.url, manifest.launchAsset.hash, manifest.extra.expoClient.version);' "$body")
[ "$served_version" = "$RELEASE_VERSION" ] || { echo "the update calls itself version $served_version, the release tag is $RELEASE_VERSION" >&2; exit 1; }
served=$(curl -sf --max-time 60 "$url" | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '=')
[ "$served" = "$hash" ] || { echo "the bundle at $url hashes to $served, the manifest says $hash" >&2; exit 1; }
echo "prod serves the update for runtime $RUNTIME; its bundle matches the manifest"
