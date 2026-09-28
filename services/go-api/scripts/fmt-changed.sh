#!/usr/bin/env bash
set -uo pipefail

check=0
base_ref=""
explicit_base=0
for arg in "$@"; do
  case "$arg" in
    --check) check=1 ;;
    *) base_ref="$arg"; explicit_base=1 ;;
  esac
done

command -v golangci-lint >/dev/null 2>&1 || {
  echo "fmt-changed: golangci-lint not installed" >&2
  exit 3
}

if [ "$explicit_base" -eq 0 ]; then
  if git rev-parse --verify --quiet gitea/main >/dev/null; then
    base_ref=gitea/main
  else
    base_ref=origin/main
  fi
fi

git rev-parse --verify --quiet "$base_ref" >/dev/null 2>&1 || {
  echo "fmt-changed: base ref '${base_ref}' not found" >&2
  exit 2
}

base=$(git merge-base "$base_ref" HEAD) || {
  echo "fmt-changed: no merge base with ${base_ref}" >&2
  exit 2
}

prefix=$(git rev-parse --show-prefix)

diff_out=$(git diff --name-only --diff-filter=ACMR "$base" -- '*.go') || {
  echo "fmt-changed: git diff failed" >&2
  exit 2
}

untracked_out=$(git ls-files --others --exclude-standard -- '*.go') || {
  echo "fmt-changed: git ls-files failed" >&2
  exit 2
}

files=()
while IFS= read -r f; do
  [ -n "$f" ] || continue
  if [ -n "$prefix" ]; then
    case "$f" in
      "$prefix"*) files+=("${f#"$prefix"}") ;;
    esac
  else
    files+=("$f")
  fi
done <<< "$diff_out"

while IFS= read -r f; do
  [ -n "$f" ] || continue
  files+=("$f")
done <<< "$untracked_out"

if [ "${#files[@]}" -eq 0 ]; then
  echo "fmt-changed: nothing to format"
  exit 0
fi

if [ "$check" -eq 1 ]; then
  golangci-lint fmt -c .golangci.strict.yml --diff "${files[@]}"
  exit $?
fi

golangci-lint fmt -c .golangci.strict.yml "${files[@]}"
