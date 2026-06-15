#!/usr/bin/env bash
# pre-tool-file-guard: blocks Write/Edit/MultiEdit on sensitive or sacred paths.
# Reads JSON event payload on stdin; emits JSON decision on stdout. Exit 2 to block.
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

# Read the tool event payload from stdin
PAYLOAD="$(cat || true)"

# Extract tool name and the target file path. jq if available; fallback parser otherwise.
TOOL_NAME="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("tool_name",""))' 2>/dev/null || echo "")"
FILE_PATH="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);t=d.get("tool_input",{});print(t.get("file_path") or t.get("path") or "")' 2>/dev/null || echo "")"
PROMPT="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("user_prompt",""))' 2>/dev/null || echo "")"

if [[ -z "$FILE_PATH" ]]; then
  exit 0
fi

block() {
  local reason="$1"
  echo "[file-guard] BLOCKED $FILE_PATH: $reason" >> "$LOG"
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
}

# 1. Block .env files (any variant)
if [[ "$FILE_PATH" =~ (^|/)\.env(\.|$) ]] && [[ "$FILE_PATH" != *".env.example" ]]; then
  block "Refusing to edit .env file (secret leak risk). Update .env.example instead and document the var."
fi

# 2. Block secrets directories
if [[ "$FILE_PATH" == *"/secrets/"* ]] || [[ "$FILE_PATH" == *"/credentials/"* ]]; then
  block "Refusing to edit files under secrets/ or credentials/."
fi

# 3. Block private keys
if [[ "$FILE_PATH" =~ \.(pem|key|p12|pfx)$ ]]; then
  block "Refusing to edit cryptographic key file."
fi

# 4. Sacred-tests rule
is_test_file=false
if [[ "$FILE_PATH" == *"/tests/"* ]] || [[ "$FILE_PATH" == *"/__tests__/"* ]] || \
   [[ "$FILE_PATH" == *".test.ts" ]] || [[ "$FILE_PATH" == *".test.tsx" ]] || \
   [[ "$FILE_PATH" == *"_test.py" ]] || [[ "$FILE_PATH" == *"/test_"*".py" ]]; then
  is_test_file=true
fi

if $is_test_file; then
  # Allowed if: prompt explicitly contains [ALLOW-TEST-EDIT: <reason>]
  if [[ "$PROMPT" =~ \[ALLOW-TEST-EDIT: ]]; then
    echo "[file-guard] ALLOW test edit: $FILE_PATH (explicit override)" >> "$LOG"
    exit 0
  fi
  # Allowed if it's a Write to a brand-new test file (adding tests is fine)
  if [[ "$TOOL_NAME" == "Write" ]] && [[ ! -f "$FILE_PATH" ]]; then
    echo "[file-guard] ALLOW new test file: $FILE_PATH" >> "$LOG"
    exit 0
  fi
  block "Sacred-tests rule: editing $FILE_PATH requires [ALLOW-TEST-EDIT: <reason>] in your prompt. Default behavior: fix implementation, not test."
fi

# 5. Block shipped migrations
if [[ "$FILE_PATH" == *"/migrations/"* ]] || [[ "$FILE_PATH" == *"/alembic/versions/"* ]]; then
  if [[ "$TOOL_NAME" != "Write" ]]; then
    # Edits to existing migrations are dangerous
    if [[ -f "$FILE_PATH" ]]; then
      if [[ "$PROMPT" =~ \[ALLOW-MIGRATION-EDIT: ]]; then
        echo "[file-guard] ALLOW migration edit: $FILE_PATH (explicit override)" >> "$LOG"
        exit 0
      fi
      block "Shipped migrations are immutable. Add a NEW migration for the correction. Override with [ALLOW-MIGRATION-EDIT: <reason>] if truly local-only branch work."
    fi
  fi
fi

exit 0
