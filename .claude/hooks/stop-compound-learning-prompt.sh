#!/usr/bin/env bash
# stop-compound-learning-prompt: at Stop, if non-trivial work happened, nudge to run /compound-learning.
# "Non-trivial" heuristic: ≥2 file edits OR new file created OR test added.
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"
cd "$PROJECT_DIR" 2>/dev/null || exit 0

CHANGED_COUNT="$(git diff --name-only HEAD 2>/dev/null | wc -l | tr -d ' ')"
NEW_FILES="$(git status --porcelain 2>/dev/null | grep -c '^??' || echo 0)"
TEST_CHANGES="$(git diff --name-only HEAD 2>/dev/null | grep -cE '(/tests/|__tests__|_test\.py|\.test\.tsx?$)' || echo 0)"

NON_TRIVIAL=false
if [[ "$CHANGED_COUNT" -ge 2 ]] || [[ "$NEW_FILES" -ge 1 ]] || [[ "$TEST_CHANGES" -ge 1 ]]; then
  NON_TRIVIAL=true
fi

if ! $NON_TRIVIAL; then exit 0; fi

MSG="[compound-learning] This session produced non-trivial work ($CHANGED_COUNT changed, $NEW_FILES new, $TEST_CHANGES tests touched).

Before ending, briefly answer:
  - Did anything surprise you this session that future-you should know?
  - Is there a *pattern* (not a bug instance) worth recording?

If yes → run /compound-learning to capture in docs/solutions/.
If no → skip. (Routine work doesn't need a learning entry.)"

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
