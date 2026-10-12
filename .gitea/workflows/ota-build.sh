#!/usr/bin/env bash
set -euo pipefail
cd "$WT/client"
rm -rf dist-ota "$WT/ota-release"
mkdir -p "$WT/ota-release"
if [ "$(uname -m)" != x86_64 ]; then
  command -v qemu-x86_64-static >/dev/null || { echo "hermesc ships for x86-64 only; install qemu-user-static" >&2; exit 1; }
  hermesc="$PWD/node_modules/hermes-compiler/hermesc/linux64-bin/hermesc"
  [ -x "$hermesc" ] || { echo "no hermesc at $hermesc" >&2; exit 1; }
  mkdir -p "$WT/ota-hermes/build/bin"
  printf '#!/bin/sh\nexec qemu-x86_64-static %s "$@"\n' "$hermesc" >"$WT/ota-hermes/build/bin/hermesc"
  chmod +x "$WT/ota-hermes/build/bin/hermesc"
  export REACT_NATIVE_OVERRIDE_HERMES_DIR="$WT/ota-hermes"
fi
tag=$(git -C "$WT" describe --tags --abbrev=0 --match 'v*') || { echo "no v* release tag below this commit" >&2; exit 1; }
VERSION=${tag#v} node -e '
  const fs = require("fs");
  const app = JSON.parse(fs.readFileSync("app.json", "utf8"));
  app.expo.version = process.env.VERSION;
  fs.writeFileSync("app.json", JSON.stringify(app, null, 2));'
runtime=$(npx expo-updates runtimeversion:resolve --platform ios | tail -1 | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).runtimeVersion))')
[ -n "$runtime" ] || { echo "could not resolve the runtime version" >&2; exit 1; }
npx expo export --platform ios --output-dir dist-ota --clear
npx expo config --json --type public >"$WT/ota-release/expo-config.json"
OTA_RUNTIME_VERSION=$runtime node scripts/ota-update.mjs dist-ota "$WT/ota-release/stage" "$WT/ota-release/expo-config.json"
tar -czf "$WT/ota-release/update.tgz" -C "$WT/ota-release/stage" .
echo "runtime=$runtime" >>"$GITHUB_OUTPUT"
echo "version=${tag#v}" >>"$GITHUB_OUTPUT"
echo "built the ios update for runtime $runtime"
