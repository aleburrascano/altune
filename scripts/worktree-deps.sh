#!/usr/bin/env bash
set -uo pipefail

log() { echo "worktree-deps: $*" >&2; }

node_major_version() {
  "$1" -e 'console.log(process.versions.node.split(".")[0])' 2>/dev/null
}

path_node_is_22_or_newer() {
  command -v node >/dev/null 2>&1 || return 1
  local major
  major=$(node_major_version node)
  [ -n "$major" ] && [ "$major" -ge 22 ] 2>/dev/null
}

newest_nvm_node22_bin() {
  local best=""
  for d in "$HOME"/.nvm/versions/node/v*/; do
    [ -d "$d" ] || continue
    local v=${d%/} major
    v=${v##*/v}
    major=${v%%.*}
    [ "$major" -ge 22 ] 2>/dev/null || continue
    best=$(printf '%s\n%s\n' "$best" "${d%/}" | sed '/^$/d' | sort -V | tail -1)
  done
  [ -n "$best" ] || return 1
  echo "$best/bin"
}

find_node22_bin() {
  if path_node_is_22_or_newer; then
    dirname "$(command -v node)"
    return 0
  fi
  newest_nvm_node22_bin
}

node_modules_matches_lockfile() { (cd "$1" && npm ls --depth=0) >/dev/null 2>&1; }

symlink_shared_node_modules() {
  rm -rf "$1/node_modules"
  ln -s "$main_tree/$1/node_modules" "$1/node_modules"
  local exclude
  exclude="$(git rev-parse --git-common-dir)/info/exclude"
  grep -qx 'node_modules' "$exclude" 2>/dev/null || echo 'node_modules' >>"$exclude"
}

node_bin=$(find_node22_bin) || { log "Node >= 22 not found"; exit 3; }
export PATH="$node_bin:$PATH"
echo "export PATH=$node_bin:\$PATH"

main_tree=$(git worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')

[ $# -eq 0 ] && set -- . apps/mobile

for dir in "$@"; do
  [ -f "$dir/package-lock.json" ] || continue
  if [ -d "$dir/node_modules" ] && node_modules_matches_lockfile "$dir"; then
    log "$dir: node_modules already complete"
    continue
  fi
  if [ -n "$main_tree" ] && [ -d "$main_tree/$dir/node_modules" ] && node_modules_matches_lockfile "$main_tree/$dir"; then
    log "$dir: borrowing node_modules from $main_tree/$dir"
    symlink_shared_node_modules "$dir"
    continue
  fi
  log "$dir: running npm ci --ignore-scripts"
  if ! (cd "$dir" && npm ci --ignore-scripts) 1>&2; then
    log "$dir: npm ci failed"
    exit 1
  fi
done
