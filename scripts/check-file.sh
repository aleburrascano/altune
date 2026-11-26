#!/usr/bin/env bash
set -uo pipefail

usage="usage: bash scripts/check-file.sh <path>
Runs this repo's fast checks on one changed file, scoped to that file or its Go package.
Exit 0: clean, or nothing to check for this kind of file (no output).
Exit 1: the file fails a check (findings on stderr).
Exit 2: usage error.
Exit 3: a check could not run because a tool or dependency is missing (stderr says which)."

if [ $# -ne 1 ] || [ -z "$1" ]; then echo "$usage" >&2; exit 2; fi
[ -f "$1" ] || { echo "check-file: $1: no such file" >&2; exit 2; }

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P) || exit 3
abs=$(realpath "$1") || exit 2
case $abs in
  "$root"/*) rel=${abs#"$root"/} ;;
  *) echo "check-file: $1 is outside this checkout ($root)" >&2; exit 2 ;;
esac
[ "$(git -C "$(dirname "$abs")" rev-parse --show-toplevel 2>/dev/null)" = "$root" ] \
  || { echo "check-file: $1 belongs to another checkout; run that checkout's scripts/check-file.sh" >&2; exit 2; }
cd "$root" || exit 3

go_api=services/go-api
work=$(mktemp -d) || exit 3
trap 'rm -rf "$work"' EXIT

nearest_dir_with() {
  local dir
  dir=$(dirname "$rel")
  while [ "$dir" != . ]; do
    for name in "$@"; do
      [ -f "$dir/$name" ] && { echo "$dir"; return 0; }
    done
    dir=$(dirname "$dir")
  done
  return 1
}

go_tool_ignores() {
  grep -qE '(^|/)(testdata|vendor|[._][^/]*)/' <<<"$1"
}

missing() {
  echo "$1 is not installed or not on PATH. $2"
  return 3
}

strip_root() {
  local line
  while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "${line//"$root/"/}"; done
}

comment_hint="Comments are banned; say it in a name, a type or a test."

go_lint() {
  local module=$1 pkg=$2 with_tests=false
  case $rel in *_test.go) with_tests=true ;; esac
  command -v golangci-lint >/dev/null || missing golangci-lint "Install it with: bash scripts/ci/go-tools.sh" || return
  (cd "$module" && GOLANGCI_LINT_CACHE="$root/.cache/golangci-lint" golangci-lint run \
    --config .golangci.strict.yml --fast-only --new-from-rev=HEAD --tests=$with_tests \
    --allow-parallel-runners --show-stats=false --path-prefix "$module" "./$pkg/") 2>&1 || return 1
}

go_vet() {
  local module=$1 pkg=$2
  (cd "$module" && go vet "./$pkg/") 2>&1 | strip_root
  [ "${PIPESTATUS[0]}" = 0 ] || return 1
}

comment_check() {
  go -C "$go_api" run "$@" 2>&1 | strip_root | grep -vE '^(comment violations: 0|exit status [0-9]+)$'
  [ "${PIPESTATUS[0]}" = 0 ] || { echo "$comment_hint"; return 1; }
}

name_check() {
  local in_module=${rel#"$go_api"/} out rc=0
  out=$(go -C "$go_api" run ./scripts/lintnames HEAD 2>&1) || rc=$?
  if [ "$rc" -gt 1 ]; then
    printf '%s\n' "$out" | strip_root
    return 1
  fi
  out=$(grep -F "  $in_module:" <<<"$out" | grep -F "vague identifier" || true)
  [ -z "$out" ] || { printf '%s\n' "$out"; return 1; }
}

js_check() {
  local check=$1 project=$2
  command -v node >/dev/null || missing node "Install the Node version in .nvmrc." || return
  [ -d "$project/node_modules" ] || { echo "$project/node_modules is missing. Run: bash scripts/worktree-deps.sh $project"; return 3; }
  node scripts/check-file.mjs "$check" "$project" "$rel" 2>&1
}

pids=()
names=()
start() {
  local name=$1; shift
  names+=("$name")
  "$@" >"$work/${#names[@]}.out" &
  pids+=($!)
}

needs_go() { command -v go >/dev/null || { missing go "Install the Go version in $go_api/go.mod." >"$work/go.out"; return 1; }; }

case $rel in
  *.go)
    if ! go_tool_ignores "$rel" && module=$(nearest_dir_with go.mod) && needs_go; then
      pkg=$(dirname "${rel#"$module"/}")
      if [ -f "$module/.golangci.strict.yml" ]; then
        start "golangci-lint (fast linters and format, lines changed since HEAD)" go_lint "$module" "$pkg"
        case $rel in *_test.go) ;; "$go_api"/*) start "no new vague names" name_check ;; esac
      else
        start "go vet" go_vet "$module" "$pkg"
      fi
      start "no comments" comment_check ./scripts/lintcomments --all "$abs"
    fi
    ;;
  *)
    if project=$(nearest_dir_with eslint.config.js eslint.config.mjs eslint.config.cjs eslint.config.ts) \
      && grep -qE '\.(ts|tsx|js|jsx|mjs|cjs)$' <<<"$rel"; then
      start "eslint" js_check eslint "$project"
      start "prettier" js_check prettier "$project"
    fi
    ;;
esac

failed=0
missing_tool=0
for i in "${!pids[@]}"; do
  wait "${pids[$i]}"
  rc=$?
  out="$work/$((i + 1)).out"
  case $rc in
    0) ;;
    3) missing_tool=1; echo "check-file: could not run ${names[$i]} on $rel: $(cat "$out")" >&2 ;;
    *) failed=1; { echo "check-file: $rel fails ${names[$i]}:"; cat "$out"; } >&2 ;;
  esac
done
[ -s "$work/go.out" ] && { missing_tool=1; echo "check-file: could not check $rel: $(cat "$work/go.out")" >&2; }

[ $failed = 1 ] && exit 1
[ $missing_tool = 1 ] && exit 3
exit 0
