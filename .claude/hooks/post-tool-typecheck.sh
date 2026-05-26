#!/usr/bin/env bash
# post-tool-typecheck: runs tsc/mypy on touched files. Surfaces errors as additionalContext.
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

PAYLOAD="$(cat || true)"
FILE_PATH="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);t=d.get("tool_input",{});print(t.get("file_path") or t.get("path") or "")' 2>/dev/null || echo "")"

if [[ -z "$FILE_PATH" ]] || [[ ! -f "$FILE_PATH" ]]; then exit 0; fi

OUTPUT=""
STATUS=0

if [[ "$FILE_PATH" == *.py ]]; then
  if command -v uv >/dev/null 2>&1 && [[ -f "${CLAUDE_PROJECT_DIR:-.}/services/api/pyproject.toml" ]]; then
    pushd "${CLAUDE_PROJECT_DIR:-.}/services/api" >/dev/null
    if OUTPUT="$(uv run --quiet mypy "$FILE_PATH" 2>&1)"; then
      STATUS=0
    else
      STATUS=$?
    fi
    popd >/dev/null
  fi
elif [[ "$FILE_PATH" == *.ts ]] || [[ "$FILE_PATH" == *.tsx ]]; then
  if [[ -f "${CLAUDE_PROJECT_DIR:-.}/apps/mobile/tsconfig.json" ]]; then
    pushd "${CLAUDE_PROJECT_DIR:-.}/apps/mobile" >/dev/null
    if command -v pnpm >/dev/null 2>&1; then
      if OUTPUT="$(pnpm exec tsc --noEmit 2>&1)"; then STATUS=0; else STATUS=$?; fi
    elif command -v npx >/dev/null 2>&1; then
      if OUTPUT="$(npx --no-install tsc --noEmit 2>&1)"; then STATUS=0; else STATUS=$?; fi
    fi
    popd >/dev/null
  fi
fi

if [[ $STATUS -ne 0 ]] && [[ -n "$OUTPUT" ]]; then
  echo "[typecheck] FAIL $FILE_PATH" >> "$LOG"
  echo "$OUTPUT" >> "$LOG"
  # Truncate to 4000 chars for context injection
  TRIMMED="$(printf '%s' "$OUTPUT" | head -c 4000)"
  jq -n --arg msg "Typecheck failed on $FILE_PATH:\n$TRIMMED" '{
    hookSpecificOutput: { hookEventName: "PostToolUse", additionalContext: $msg }
  }' 2>/dev/null || cat <<EOF
{
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "Typecheck failed on $FILE_PATH (see .claude/hooks.log for details)"
  }
}
EOF
fi

exit 0
