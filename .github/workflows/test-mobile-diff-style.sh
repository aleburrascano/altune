#!/usr/bin/env bash

set -euo pipefail

if [ "$EVENT" = "pull_request" ] && [ -n "$PR_BASE" ]; then
  BASE="$PR_BASE"
elif [ "$EVENT" = "push" ] && [ -n "$BEFORE" ] \
  && [ "$BEFORE" != "0000000000000000000000000000000000000000" ] \
  && git cat-file -e "${BEFORE}^{commit}" 2>/dev/null; then
  BASE="$BEFORE"
else
  echo "No usable diff base ($EVENT); skipping diff-scoped style enforcement."
  exit 0
fi
echo "Diff base: $BASE"
node scripts/lint-changed-lines.mjs "$BASE"
