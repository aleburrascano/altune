#!/usr/bin/env bash
set -uo pipefail

command -v jq >/dev/null || { echo "check-edit hook: jq is not installed, so this edit was not checked. Install jq." >&2; exit 2; }

path=$(jq -er '.tool_input.file_path | select(type == "string" and . != "")') \
  || { echo "check-edit hook: the hook input has no tool_input.file_path, so this edit was not checked" >&2; exit 2; }

dir=$(dirname "$path")
[ -d "$dir" ] || exit 0

own_repo=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --path-format=absolute --git-common-dir) || exit 2
file_repo=$(git -C "$dir" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || exit 0
[ "$file_repo" = "$own_repo" ] || exit 0

root=$(git -C "$dir" rev-parse --show-toplevel) || exit 2
[ -f "$root/scripts/check-file.sh" ] || exit 0

bash "$root/scripts/check-file.sh" "$path" >&2 || exit 2
