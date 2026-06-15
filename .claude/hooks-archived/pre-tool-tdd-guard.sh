#!/usr/bin/env bash
# pre-tool-tdd-guard: blocks writes to production code if no failing test exists for the change.
# Pragmatic heuristic, not a full TDD-Guard implementation. Allows bypass via [ALLOW-NO-TEST: <reason>].
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

PAYLOAD="$(cat || true)"

TOOL_NAME="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("tool_name",""))' 2>/dev/null || echo "")"
FILE_PATH="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);t=d.get("tool_input",{});print(t.get("file_path") or t.get("path") or "")' 2>/dev/null || echo "")"
PROMPT="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("user_prompt",""))' 2>/dev/null || echo "")"

if [[ -z "$FILE_PATH" ]]; then exit 0; fi

# Only guard production source files
is_prod_src=false
if [[ "$FILE_PATH" == *"/services/api/src/"* ]] || [[ "$FILE_PATH" == *"/apps/mobile/src/"* ]]; then
  # Exclude test files, __init__.py, _template/
  if [[ "$FILE_PATH" != *"/tests/"* ]] && [[ "$FILE_PATH" != *"/__tests__/"* ]] && \
     [[ "$FILE_PATH" != *"_template/"* ]] && [[ "$FILE_PATH" != *"__init__.py" ]]; then
    is_prod_src=true
  fi
fi

if ! $is_prod_src; then exit 0; fi

# Explicit bypass
if [[ "$PROMPT" =~ \[ALLOW-NO-TEST: ]]; then
  echo "[tdd-guard] ALLOW no-test for $FILE_PATH (explicit override)" >> "$LOG"
  exit 0
fi

# Heuristic: look for a sibling test file
relative="${FILE_PATH#*/services/api/src/}"
relative="${relative#*/apps/mobile/src/}"
basename="$(basename "$FILE_PATH")"
stem="${basename%.*}"

# Python: tests/unit/<package>/test_<module>.py
# TS: <feature>/__tests__/<module>.test.ts
test_candidates=(
  "${CLAUDE_PROJECT_DIR:-.}/services/api/tests/unit/altune/${relative%/*}/test_${stem}.py"
  "${CLAUDE_PROJECT_DIR:-.}/services/api/tests/integration/altune/${relative%/*}/test_${stem}.py"
  "${CLAUDE_PROJECT_DIR:-.}/apps/mobile/src/${relative%/*}/__tests__/${stem}.test.ts"
  "${CLAUDE_PROJECT_DIR:-.}/apps/mobile/src/${relative%/*}/__tests__/${stem}.test.tsx"
)

found_test=false
for candidate in "${test_candidates[@]}"; do
  if [[ -f "$candidate" ]]; then
    found_test=true
    break
  fi
done

if $found_test; then
  echo "[tdd-guard] PASS for $FILE_PATH (test exists)" >> "$LOG"
  exit 0
fi

# No test found. Warn but don't block on day-1 scaffolding (allow Write on greenfield files).
# Block on Edit (modifying existing code without a test is the discipline breach).
if [[ "$TOOL_NAME" == "Write" ]] && [[ ! -f "$FILE_PATH" ]]; then
  # Greenfield write — soft-warn via stderr, but allow
  echo "[tdd-guard] WARN new file $FILE_PATH has no companion test. Consider TDD-first." >&2
  exit 0
fi

# Edit on file with no test — block
reason="No companion test for $FILE_PATH. TDD discipline: write a failing test first. Override with [ALLOW-NO-TEST: <reason>] in your prompt if this edit is non-behavioral (rename, formatting)."
echo "[tdd-guard] BLOCK $FILE_PATH ($reason)" >> "$LOG"
cat <<EOF
{
  "decision": "block",
  "reason": "$reason",
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "$reason"
  }
}
EOF
exit 2
