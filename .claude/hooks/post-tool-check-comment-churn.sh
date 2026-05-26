#!/usr/bin/env bash
# post-tool-check-comment-churn: flags removal of AIDEV-* anchor comments (durable annotations).
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

PAYLOAD="$(cat || true)"
FILE_PATH="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);t=d.get("tool_input",{});print(t.get("file_path") or t.get("path") or "")' 2>/dev/null || echo "")"

if [[ -z "$FILE_PATH" ]] || [[ ! -f "$FILE_PATH" ]]; then exit 0; fi

# Compare current file against last committed version (if tracked)
cd "${CLAUDE_PROJECT_DIR:-.}" 2>/dev/null || exit 0

if ! git ls-files --error-unmatch "$FILE_PATH" >/dev/null 2>&1; then
  exit 0  # untracked file; nothing to compare
fi

# Count AIDEV-* anchors in HEAD vs working tree
HEAD_COUNT="$(git show "HEAD:$FILE_PATH" 2>/dev/null | grep -cE 'AIDEV-(NOTE|DECISION|WARNING|TODO)' || echo 0)"
NOW_COUNT="$(grep -cE 'AIDEV-(NOTE|DECISION|WARNING|TODO)' "$FILE_PATH" || echo 0)"

if [[ "$NOW_COUNT" -lt "$HEAD_COUNT" ]]; then
  DIFF=$(( HEAD_COUNT - NOW_COUNT ))
  echo "[comment-churn] $FILE_PATH lost $DIFF AIDEV-* anchor(s)" >> "$LOG"
  cat <<EOF
{
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "⚠️ $FILE_PATH lost $DIFF AIDEV-* anchor comment(s). These are durable annotations — restore them unless removal is intentional and documented."
  }
}
EOF
fi

exit 0
