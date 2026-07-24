#!/usr/bin/env bash
# claude-md-staleness-check: blocks commits that add, delete or rename a file
# under a CLAUDE.md's scope without updating that CLAUDE.md. Called from
# .husky/pre-commit.
#
# Scope: a nested CLAUDE.md governs its own directory down to the next nested
# CLAUDE.md (the same "nearest ancestor" rule Claude Code uses to load them).
#
# Trigger: structural changes only (added / deleted / renamed). These docs are
# file maps, test-file lists and pattern notes — a new or removed file makes
# them stale, a one-line edit inside an already-listed file usually does not.
# Plain modifications are the OKF bundle's job, not this hook's.
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

# The root CLAUDE.md documents project-wide rules, not a file map — it never
# owns a scope. `_template` is a scaffold copied per feature, not live code.
EXCLUDED_OWNERS=(
  "CLAUDE.md"
  "apps/mobile/src/features/_template/CLAUDE.md"
)

is_excluded_owner() {
  local owner="$1" e
  for e in "${EXCLUDED_OWNERS[@]}"; do
    [ "$owner" = "$e" ] && return 0
  done
  return 1
}

# Files that never describe behavior a context doc would document.
is_ignorable_trigger() {
  local file="$1"
  case "${file##*/}" in
    CLAUDE.md) return 0 ;;
  esac
  case "$file" in
    */__snapshots__/*|*.snap) return 0 ;;
  esac
  return 1
}

# Nearest ancestor directory holding a CLAUDE.md, printed as its path.
# Empty when the file sits outside every nested scope.
owner_of() {
  local dir
  dir=$(dirname "$1")
  while [ "$dir" != "." ] && [ "$dir" != "/" ]; do
    if [ -f "$dir/CLAUDE.md" ] && ! is_excluded_owner "$dir/CLAUDE.md"; then
      printf '%s\n' "$dir/CLAUDE.md"
      return 0
    fi
    dir=$(dirname "$dir")
  done
  return 0
}

staged_all=()
while IFS= read -r line; do
  [ -n "$line" ] && staged_all+=("$line")
done < <(git diff --cached --name-only)

if [ "${#staged_all[@]}" -eq 0 ]; then
  exit 0
fi

is_staged() {
  local target="$1" f
  for f in "${staged_all[@]}"; do
    [ "$f" = "$target" ] && return 0
  done
  return 1
}

# Structural changes only. Renames report both paths; both sides count, since a
# rename removes a file from one scope and adds it to another.
triggers=()
reasons=()
record() {
  local file="$1" reason="$2"
  is_ignorable_trigger "$file" && return 0
  triggers+=("$file")
  reasons+=("$reason")
}

while IFS=$'\t' read -r status path_a path_b; do
  [ -n "${status:-}" ] || continue
  case "$status" in
    A) record "$path_a" "added" ;;
    D) record "$path_a" "deleted" ;;
    R*)
      record "$path_a" "renamed away"
      record "${path_b:-}" "renamed in"
      ;;
  esac
done < <(git diff --cached --name-status --diff-filter=ADR)

if [ "${#triggers[@]}" -eq 0 ]; then
  exit 0
fi

blocked=()
i=0
while [ "$i" -lt "${#triggers[@]}" ]; do
  file="${triggers[$i]}"
  reason="${reasons[$i]}"
  i=$((i + 1))

  [ -n "$file" ] || continue

  owner=$(owner_of "$file")
  [ -n "$owner" ] || continue
  is_staged "$owner" && continue

  blocked+=("$file ($reason) but $owner was not updated")
done

if [ "${#blocked[@]}" -gt 0 ]; then
  echo "CLAUDE.md staleness check failed:" >&2
  for msg in "${blocked[@]}"; do
    echo "  - $msg" >&2
  done
  echo "" >&2
  echo "A file entered or left the scope, so the context doc's file map, test list" >&2
  echo "or dependency notes are likely stale. Update the CLAUDE.md and restage it." >&2
  echo "If the doc genuinely needs no change, commit with --no-verify." >&2
  exit 1
fi

exit 0
