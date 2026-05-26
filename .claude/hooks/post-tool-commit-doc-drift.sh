#!/usr/bin/env bash
# post-tool-commit-doc-drift: after a `git commit` Bash invocation, check if code changed
# without touching expected doc artifacts; surface as warning + suggest /update-docs-freshness.
set -euo pipefail

LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/hooks.log"
DRIFT_LOG="${CLAUDE_PROJECT_DIR:-.}/.claude/doc-drift.log"
mkdir -p "$(dirname "$LOG")"

PAYLOAD="$(cat || true)"

TOOL_NAME="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("tool_name",""))' 2>/dev/null || echo "")"
COMMAND="$(printf '%s' "$PAYLOAD" | python -c 'import sys,json;d=json.load(sys.stdin);print(d.get("tool_input",{}).get("command",""))' 2>/dev/null || echo "")"

# Only run on git commit
if [[ "$TOOL_NAME" != "Bash" ]] || [[ ! "$COMMAND" =~ git[[:space:]]+commit ]]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}" 2>/dev/null || exit 0

# Check if the commit message had ALLOW-DRIFT override
LAST_MSG="$(git log -1 --pretty=%B 2>/dev/null || echo '')"
if [[ "$LAST_MSG" =~ \[ALLOW-DRIFT: ]]; then
  echo "[doc-drift] OVERRIDE on $(git rev-parse HEAD): $LAST_MSG" >> "$DRIFT_LOG"
  exit 0
fi

CHANGED="$(git diff-tree --no-commit-id --name-only -r HEAD 2>/dev/null || echo '')"
if [[ -z "$CHANGED" ]]; then exit 0; fi

WARNINGS=()

# Heuristic 1: code in services/api/src/altune/<context>/ changed but docs/specs/<context>/ not touched
while IFS= read -r f; do
  if [[ "$f" =~ services/api/src/altune/(domain|application|adapters)/([^/]+)/ ]]; then
    context="${BASH_REMATCH[2]}"
    spec_dir="docs/specs/$context"
    if [[ -d "$spec_dir" ]] && ! echo "$CHANGED" | grep -q "^$spec_dir/"; then
      WARNINGS+=("Code in $context/ changed; $spec_dir/ not touched.")
    fi
  fi
  if [[ "$f" =~ apps/mobile/src/features/([^/]+)/ ]]; then
    feat="${BASH_REMATCH[1]}"
    spec_dir="docs/specs/$feat"
    if [[ -d "$spec_dir" ]] && ! echo "$CHANGED" | grep -q "^$spec_dir/"; then
      WARNINGS+=("Mobile feature $feat changed; $spec_dir/ not touched.")
    fi
  fi
done <<<"$CHANGED"

# Heuristic 2: new domain types may need ubiquitous-language.md entry (warned; not exhaustive)
if echo "$CHANGED" | grep -q "services/api/src/altune/domain/" && ! echo "$CHANGED" | grep -q "docs/ubiquitous-language.md"; then
  WARNINGS+=("Domain changes detected; check docs/ubiquitous-language.md for new terms.")
fi

if [[ ${#WARNINGS[@]} -gt 0 ]]; then
  SHA="$(git rev-parse --short HEAD)"
  echo "[doc-drift] $SHA flagged:" >> "$DRIFT_LOG"
  for w in "${WARNINGS[@]}"; do echo "  - $w" >> "$DRIFT_LOG"; done

  MSG="⚠️ Doc drift after commit $SHA:\n"
  for w in "${WARNINGS[@]}"; do MSG+="  - $w\n"; done
  MSG+="\nRun /update-docs-freshness to address, or amend with [ALLOW-DRIFT: <reason>] in commit body."

  python - "$MSG" <<'PYEOF'
import json, sys
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "PostToolUse",
        "additionalContext": sys.argv[1]
    }
}))
PYEOF
fi

exit 0
