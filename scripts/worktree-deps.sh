#!/usr/bin/env bash
set -uo pipefail

log() { echo "worktree-deps: $*" >&2; }

root=$(git rev-parse --show-toplevel 2>/dev/null) || { log "run inside the altune checkout or one of its worktrees"; exit 3; }
cd "$root" || exit 3

wanted_node_major() {
  local v
  v=$(tr -d '[:space:]vV' <.nvmrc 2>/dev/null)
  v=${v%%.*}
  if [[ $v =~ ^[0-9]+$ ]]; then echo "$v"; else echo 22; fi
}

node_major_version() {
  "$1" -e 'console.log(process.versions.node.split(".")[0])' 2>/dev/null
}

path_node_major() {
  command -v node >/dev/null 2>&1 || return 1
  node_major_version node
}

nvm_node_bin() {
  local best
  best=$(printf '%s\n' "$HOME"/.nvm/versions/node/v*/ | while read -r d; do
    [ -x "${d}bin/node" ] || continue
    local v=${d%/}
    v=${v##*/v}
    "$1" "${v%%.*}" && echo "${d%/}"
  done | sort -V | tail -1)
  [ -n "$best" ] || return 1
  echo "$best/bin"
}

is_wanted() { [ "$1" = "$want_major" ] 2>/dev/null; }
is_newer() { [ "$1" -gt "$want_major" ] 2>/dev/null; }

find_node_bin() {
  local major
  major=$(path_node_major)
  if is_wanted "$major"; then
    dirname "$(command -v node)"
  elif nvm_node_bin is_wanted; then
    :
  elif is_newer "$major"; then
    log "Node $want_major (.nvmrc) not found; falling back to Node $major on PATH, where mobile jest may fail (nvm install $want_major)"
    dirname "$(command -v node)"
  elif nvm_node_bin is_newer; then
    log "Node $want_major (.nvmrc) not found; falling back to a newer Node under ~/.nvm, where mobile jest may fail (nvm install $want_major)"
  else
    return 1
  fi
}

node_modules_matches_lockfile() { (cd "$1" && npm ls --depth=0) >/dev/null 2>&1; }

lockfile_same_as_main() { cmp -s "$1/package-lock.json" "$main_tree/$1/package-lock.json"; }

symlink_shared_node_modules() {
  rm -rf "$1/node_modules"
  ln -sfnT "$main_tree/$1/node_modules" "$1/node_modules" || return 1
  local exclude
  exclude="$(git rev-parse --git-common-dir)/info/exclude"
  grep -qx 'node_modules' "$exclude" 2>/dev/null || echo 'node_modules' >>"$exclude"
}

want_major=$(wanted_node_major)
node_bin=$(find_node_bin) || { log "Node >= $want_major not found on PATH or under ~/.nvm; .nvmrc wants $want_major: nvm install $want_major"; exit 3; }
export PATH="$node_bin:$PATH"
echo "export PATH=$node_bin:\$PATH"

main_tree=$(git worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')

[ $# -eq 0 ] && set -- . client services/overseer/web

borrow_allowed() {
  [ -n "$main_tree" ] && [ "$(cd "$main_tree" && pwd -P)" != "$(pwd -P)" ]
}

for dir in "$@"; do
  [ -f "$dir/package-lock.json" ] || continue
  if [ -d "$dir/node_modules" ] && node_modules_matches_lockfile "$dir"; then
    log "$dir: node_modules already complete"
    continue
  fi
  if borrow_allowed && [ -d "$main_tree/$dir/node_modules" ] && lockfile_same_as_main "$dir" && node_modules_matches_lockfile "$main_tree/$dir"; then
    log "$dir: borrowing node_modules from $main_tree/$dir"
    symlink_shared_node_modules "$dir" || { log "$dir: could not link node_modules (a real directory appeared there)"; exit 1; }
    continue
  fi
  [ -L "$dir/node_modules" ] && rm "$dir/node_modules"
  log "$dir: running npm ci --ignore-scripts"
  if ! (cd "$dir" && npm ci --ignore-scripts) 1>&2; then
    log "$dir: npm ci failed"
    exit 1
  fi
done
