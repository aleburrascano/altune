#!/usr/bin/env bash
set -euo pipefail
repo=${RELEASE_REPO:-aleburrascano/altune}
if [ -L node_modules ]; then
  echo "copying the linked node_modules: the fingerprint hashes real paths, so a link outside the checkout gives a runtime no build has" >&2
  linked=$(readlink -f node_modules)
  rm node_modules
  cp -al "$linked" node_modules 2>/dev/null || { rm -rf node_modules; cp -a "$linked" node_modules; }
fi
runtime=$(npx expo-updates runtimeversion:resolve --platform ios | tail -1 | jq -r '.runtimeVersion // empty')
[ -n "$runtime" ] || { echo "could not resolve the runtime version" >&2; exit 1; }
release=$(gh release view -R "$repo" --json tagName,body) || { echo "could not read the latest iOS release of $repo" >&2; exit 1; }
tag=$(jq -r .tagName <<<"$release")
shipped=$(jq -r .body <<<"$release" | grep -oE "runtime: .[0-9a-f]+" | cut -c11- || true)
[ "$runtime" = "$shipped" ] || {
  echo "this update is for runtime $runtime, but iOS $tag runs ${shipped:-an unknown runtime}, so no installed app would load it. Release a new iOS build first." >&2
  exit 1
}
echo "$runtime"
