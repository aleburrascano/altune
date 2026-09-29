#!/usr/bin/env bash
set -uo pipefail
pr=$1
forge=${FORGE:-$(command -v forge || echo /home/ubuntu/.local/bin/forge)}
view=$("$forge" pr view "$pr" --json body,additions,deletions,files) || { echo "::warning::pr-shape could not read PR #$pr"; exit 0; }
body=$(jq -r '.body // ""' <<<"$view")
files=$(jq -r '.files[].path' <<<"$view")
size=$(jq '(.additions // 0) + (.deletions // 0)' <<<"$view")
found=0
errors=0
warn() { echo "::warning::$1"; found=$((found + 1)); }
fail() { echo "::error::$1"; errors=$((errors + 1)); }
[ "$(printf '%s' "$body" | tr -d '[:space:]' | wc -c)" -ge 20 ] \
  || warn "This PR has little or no description. A reviewer needs the why and the shape of the change, not just the diff."
if grep -qE '^services/go-api/.*\.go$|^apps/mobile/src/.*\.tsx?$' <<<"$files" \
  && ! grep -qE '_test\.go$|\.(test|spec)\.tsx?$' <<<"$files"; then
  warn "Source changed but no test file did. A behavior change should carry a test that proves it."
fi
[ "$size" -le 600 ] || warn "This PR changes $size lines. Large diffs are hard to review well and often should be split."
issue=$(grep -ioE '(closes|fixes|resolves) #[0-9]+' <<<"$body" | head -n 1 | grep -oE '[0-9]+')
if [ -z "$issue" ]; then
  warn "No Closes #N in the PR body, so the ticket's Coverage and DONE-WHEN requirements were not checked."
elif ! ticket=$("$forge" issue view "$issue" --json labels,body); then
  warn "pr-shape could not read issue #$issue, so its Coverage and DONE-WHEN requirements were not checked."
else
  labels=$(jq -r '.labels[].name' <<<"$ticket")
  if grep -qxE 'risk|complexity:deep' <<<"$labels" && ! grep -qE '^## Coverage\b' <<<"$body"; then
    fail "Issue #$issue is risk or complexity:deep, so the PR body needs a '## Coverage' section."
  fi
  done_when=$(jq -r '.body // ""' <<<"$ticket" | awk '/^## Done when/{on=1; next} /^## /{on=0} on && /^- /{print}')
  if [ -n "$done_when" ] && ! grep -qE '^DONE-WHEN:' <<<"$body"; then
    fail "Issue #$issue has Done-when bullets, so the PR body needs 'DONE-WHEN: <bullet> → <file>::<test>' lines."
  fi
fi
echo "pr-shape: $found note(s) on PR #$pr"
[ "$errors" -eq 0 ]
