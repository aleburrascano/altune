#!/usr/bin/env bash
# post-tool-test-changed: runs the tests closest to the touched file for fast feedback.
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

PAYLOAD="$(cat || true)"
FILE_PATH="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);t=d.get("tool_input",{});print(t.get("file_path") or t.get("path") or "")' 2>/dev/null || echo "")"

if [[ -z "$FILE_PATH" ]] || [[ ! -f "$FILE_PATH" ]]; then exit 0; fi

# Don't re-run tests on test edits themselves (let dev run them explicitly)
if [[ "$FILE_PATH" == *"/tests/"* ]] || [[ "$FILE_PATH" == *"/__tests__/"* ]] || \
   [[ "$FILE_PATH" == *"_test.py" ]] || [[ "$FILE_PATH" == *".test.ts" ]] || [[ "$FILE_PATH" == *".test.tsx" ]]; then
  exit 0
fi

OUTPUT=""
STATUS=0

if [[ "$FILE_PATH" == *"/services/api/src/"* ]] && [[ "$FILE_PATH" == *.py ]]; then
  basename="$(basename "$FILE_PATH")"
  stem="${basename%.*}"
  if command -v uv >/dev/null 2>&1; then
    pushd "${CLAUDE_PROJECT_DIR:-.}/services/api" >/dev/null
    # Try to find and run a matching test file
    test_file="$(find tests/unit -name "test_${stem}.py" 2>/dev/null | head -1)"
    if [[ -n "$test_file" ]]; then
      if OUTPUT="$(uv run --quiet pytest -q "$test_file" 2>&1)"; then STATUS=0; else STATUS=$?; fi
    fi
    popd >/dev/null
  fi
elif [[ "$FILE_PATH" == *"/apps/mobile/src/"* ]] && { [[ "$FILE_PATH" == *.ts ]] || [[ "$FILE_PATH" == *.tsx ]]; }; then
  basename="$(basename "$FILE_PATH")"
  stem="${basename%.*}"
  dir="$(dirname "$FILE_PATH")"
  test_file=""
  for cand in "$dir/__tests__/${stem}.test.ts" "$dir/__tests__/${stem}.test.tsx"; do
    [[ -f "$cand" ]] && test_file="$cand" && break
  done
  if [[ -n "$test_file" ]]; then
    pushd "${CLAUDE_PROJECT_DIR:-.}/apps/mobile" >/dev/null
    if command -v pnpm >/dev/null 2>&1; then
      if OUTPUT="$(pnpm exec jest --findRelatedTests "$FILE_PATH" 2>&1)"; then STATUS=0; else STATUS=$?; fi
    fi
    popd >/dev/null
  fi
fi

if [[ $STATUS -ne 0 ]] && [[ -n "$OUTPUT" ]]; then
  echo "[test-changed] FAIL $FILE_PATH" >> "$LOG"
  echo "$OUTPUT" >> "$LOG"
  TRIMMED="$(printf '%s' "$OUTPUT" | head -c 3000)"
  cat <<EOF
{
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "Tests failing for $FILE_PATH:\n$TRIMMED"
  }
}
EOF
fi

exit 0
