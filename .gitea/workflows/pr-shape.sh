#!/usr/bin/env bash
set -uo pipefail
pr=$1
forge=${FORGE:-/home/ubuntu/.local/bin/forge}
view=$("$forge" pr view "$pr" --json body,additions,deletions,files) || { echo "::warning::pr-shape could not read PR #$pr"; exit 0; }
body=$(jq -r '.body // ""' <<<"$view")
files=$(jq -r '.files[].path' <<<"$view")
size=$(jq '(.additions // 0) + (.deletions // 0)' <<<"$view")
found=0
warn() { echo "::warning::$1"; found=$((found + 1)); }
[ "$(printf '%s' "$body" | tr -d '[:space:]' | wc -c)" -ge 20 ] \
  || warn "This PR has little or no description. A reviewer needs the why and the shape of the change, not just the diff."
if grep -qE '^services/go-api/.*\.go$|^apps/mobile/src/.*\.tsx?$' <<<"$files" \
  && ! grep -qE '_test\.go$|\.(test|spec)\.tsx?$' <<<"$files"; then
  warn "Source changed but no test file did. A behavior change should carry a test that proves it."
fi
[ "$size" -le 600 ] || warn "This PR changes $size lines. Large diffs are hard to review well and often should be split."
echo "pr-shape: $found note(s) on PR #$pr"
