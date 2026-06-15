#!/usr/bin/env bash
# post-tool-lint: runs ruff/eslint on touched files. Auto-fixes when safe; surfaces remaining issues.
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
    uv run --quiet ruff check --fix "$FILE_PATH" >/dev/null 2>&1 || true
    if OUTPUT="$(uv run --quiet ruff check "$FILE_PATH" 2>&1)"; then STATUS=0; else STATUS=$?; fi
    popd >/dev/null
  fi
elif [[ "$FILE_PATH" == *.ts ]] || [[ "$FILE_PATH" == *.tsx ]] || [[ "$FILE_PATH" == *.js ]] || [[ "$FILE_PATH" == *.jsx ]]; then
  if [[ -f "${CLAUDE_PROJECT_DIR:-.}/apps/mobile/.eslintrc.cjs" ]] || [[ -f "${CLAUDE_PROJECT_DIR:-.}/apps/mobile/eslint.config.js" ]]; then
    pushd "${CLAUDE_PROJECT_DIR:-.}/apps/mobile" >/dev/null
    if command -v pnpm >/dev/null 2>&1; then
      pnpm exec eslint --fix "$FILE_PATH" >/dev/null 2>&1 || true
      if OUTPUT="$(pnpm exec eslint "$FILE_PATH" 2>&1)"; then STATUS=0; else STATUS=$?; fi
    fi
    popd >/dev/null
  fi
fi

if [[ $STATUS -ne 0 ]] && [[ -n "$OUTPUT" ]]; then
  echo "[lint] FAIL $FILE_PATH" >> "$LOG"
  echo "$OUTPUT" >> "$LOG"
  TRIMMED="$(printf '%s' "$OUTPUT" | head -c 3000)"
  cat <<EOF
{
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "Lint issues on $FILE_PATH:\n$TRIMMED"
  }
}
EOF
fi

exit 0
