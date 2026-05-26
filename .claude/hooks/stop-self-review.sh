#!/usr/bin/env bash
# stop-self-review: on Stop, if non-trivial work happened this session, suggest running
# /verify-end-to-end before declaring done.
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"
LOG="$PROJECT_DIR/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

cd "$PROJECT_DIR" 2>/dev/null || exit 0

# Use git to detect uncommitted changes (proxy for "did work happen")
UNCOMMITTED="$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')"

if [[ "$UNCOMMITTED" -eq 0 ]]; then exit 0; fi

MSG="[stop-self-review] $UNCOMMITTED uncommitted change(s) detected.

Before ending the session, consider:
  - Run /verify-end-to-end (typecheck + lint + tests)
  - If feature work: /code-review-6-aspect on the diff
  - Capture any learnings via /compound-learning
  - Commit via /git-commit with proper Conventional Commits format"

python - "$MSG" <<'PYEOF'
import json, sys
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "Stop",
        "additionalContext": sys.argv[1]
    }
}))
PYEOF

exit 0
