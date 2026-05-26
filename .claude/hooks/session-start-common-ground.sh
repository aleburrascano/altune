#!/usr/bin/env bash
# session-start-common-ground: injects a "common ground" prompt at session start so Claude
# states what it thinks the project state is, surfacing assumptions before any work begins.
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"

# Build a brief project state summary from git + filesystem
STATE=""
if cd "$PROJECT_DIR" 2>/dev/null && [[ -d .git ]]; then
  STATE+="Branch: $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')\n"
  STATE+="Recent commits:\n$(git log --oneline -5 2>/dev/null || echo '(none yet)')\n"
  STATE+="Working tree: $(git status --short 2>/dev/null | head -10 || echo 'clean')\n"
fi

ACTIVE_SPEC="$(ls -1t "$PROJECT_DIR"/docs/specs/*/spec.md 2>/dev/null | head -1 || true)"
ACTIVE_SPEC_SUMMARY=""
if [[ -n "$ACTIVE_SPEC" ]]; then
  ACTIVE_SPEC_SUMMARY="Most recent spec: $ACTIVE_SPEC"
fi

MSG=$(cat <<EOF
[session-start] Common-ground check.

Project state:
$STATE
$ACTIVE_SPEC_SUMMARY

Before doing any non-trivial work this session, briefly state:
  1) What you understand the active task to be (or "no active task")
  2) Your top 1-2 assumptions about how to proceed
  3) Anything you're unsure about

If routine work / quick question, skip this and just answer.
EOF
)

# Use python to safely JSON-encode the message
python - "$MSG" <<'PYEOF'
import json, sys
msg = sys.argv[1]
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "SessionStart",
        "additionalContext": msg
    }
}))
PYEOF

exit 0
