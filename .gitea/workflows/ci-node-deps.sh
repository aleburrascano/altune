#!/usr/bin/env bash
set -euo pipefail
dir=$1
repo=${ALTUNE_REPO:-/home/ubuntu/projects/altune}
main=$repo/$dir
cache_root=${ALTUNE_DEPS_CACHE:-$HOME/.cache/altune-ci-deps}
caches_kept=4
cd "$WT/$dir"
[ -e node_modules ] && exit 0
if [ "${ALTUNE_CI_OWNED:-}" = 1 ] && [ -x "$repo/../refresh-deps.sh" ]; then
  "$repo/../refresh-deps.sh" gitea/main
fi
same_tree() { cmp -s <(jq -S 'del(.packages[""])' package-lock.json) <(jq -S 'del(.packages[""])' "$main/package-lock.json"); }
linked_sources() { jq -r '.packages[] | select(.link == true) | .resolved' package-lock.json; }

cache_key() {
  {
    cat package-lock.json .npmrc 2>/dev/null
    linked_sources | while read -r src; do find "$src" -type f -print0 | sort -z | xargs -0 -r sha256sum; done
  } | sha256sum | cut -c1-16
}

build_cache() {
  local cache=$1 tmp=$1.tmp.$$ src
  rm -rf "$tmp" && mkdir -p "$tmp"
  for f in package.json package-lock.json .npmrc; do
    if [ -f "$f" ]; then cp "$f" "$tmp/"; fi
  done
  while read -r src; do
    mkdir -p "$tmp/$(dirname "$src")" && cp -r "$src" "$tmp/$src"
  done < <(linked_sources)
  (cd "$tmp" && npm ci --no-audit --no-fund) || { rm -rf "$tmp"; return 1; }
  mv -T "$tmp" "$cache"
  echo "$dir: installed into the cache for this lockfile"
}

prune_caches() {
  find "$cache_root" -mindepth 1 -maxdepth 1 -type d -name "${dir//\//_}-*" ! -name '*.tmp.*' -printf '%T@ %p\n' |
    sort -rn | tail -n +$((caches_kept + 1)) | cut -d' ' -f2- | xargs -r rm -rf
}

if [ -d "$main/node_modules" ] && same_tree; then
  ln -sfnT "$main/node_modules" node_modules
  echo "$dir: linked node_modules from the main checkout"
  exit 0
fi

mkdir -p "$cache_root"
cache=$cache_root/${dir//\//_}-$(cache_key)
(
  flock -w 1800 9
  [ -d "$cache/node_modules" ] || build_cache "$cache"
) 9>"$cache.lock"
touch "$cache"
ln -sfnT "$cache/node_modules" node_modules
echo "$dir: linked the cached install for this lockfile"
prune_caches || true
