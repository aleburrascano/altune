#!/usr/bin/env bash
# okf-staleness-check: blocks commits that change a concept's resource files
# without updating the concept. Called from .husky/pre-commit.
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

if [ ! -d okf ]; then
  exit 0
fi

staged_files=()
while IFS= read -r line; do
  [ -n "$line" ] || continue
  # Router CLAUDE.md files are navigation docs, not behavior — they never
  # make a concept stale, so they don't participate in resource matching.
  # (is_staged still sees concepts via the full staged list below.)
  case "${line##*/}" in
    CLAUDE.md) continue ;;
  esac
  staged_files+=("$line")
done < <(git diff --cached --name-only)

if [ "${#staged_files[@]}" -eq 0 ]; then
  exit 0
fi

is_staged() {
  local target="$1"
  local f
  for f in "${staged_files[@]}"; do
    if [ "$f" = "$target" ]; then
      return 0
    fi
  done
  return 1
}

matches_resource() {
  local resource="$1" file="$2"
  case "$resource" in
    */)
      case "$file" in
        "$resource"*) return 0 ;;
        *) return 1 ;;
      esac
      ;;
    *)
      [ "$resource" = "$file" ]
      ;;
  esac
}

blocked=()

while IFS= read -r -d '' concept; do
  case "$(basename "$concept")" in
    index.md|log.md) continue ;;
  esac

  resource_line=$(sed -n 's/^resource:[[:space:]]*//p' "$concept" | head -n1)

  if [ -z "$resource_line" ]; then
    continue
  fi

  # A concept may list several resources, comma-separated.
  IFS=',' read -ra resources <<< "$resource_line"
  for resource in "${resources[@]}"; do
    # trim whitespace and quotes
    resource="${resource#"${resource%%[![:space:]]*}"}"
    resource="${resource%"${resource##*[![:space:]]}"}"
    resource="${resource%\"}"
    resource="${resource#\"}"
    resource="${resource%\'}"
    resource="${resource#\'}"

    [ -z "$resource" ] && continue

    for file in "${staged_files[@]}"; do
      if matches_resource "$resource" "$file"; then
        if ! is_staged "$concept"; then
          blocked+=("$resource changed but $concept was not updated")
        fi
        break 2
      fi
    done
  done
done < <(find okf -type f -name '*.md' -print0)

if [ "${#blocked[@]}" -gt 0 ]; then
  echo "OKF staleness check failed:" >&2
  for msg in "${blocked[@]}"; do
    echo "  - $msg" >&2
  done
  echo "" >&2
  echo "Run the okf-staleness-fix skill (if using Claude Code) to update the concept(s), then recommit." >&2
  exit 1
fi

exit 0
