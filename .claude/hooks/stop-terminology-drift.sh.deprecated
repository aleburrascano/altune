#!/usr/bin/env bash
# stop-terminology-drift: scans changed domain/ files for class names absent from ubiquitous-language.md.
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"
LOG="$PROJECT_DIR/.claude/hooks.log"
GLOSSARY="$PROJECT_DIR/docs/ubiquitous-language.md"
mkdir -p "$(dirname "$LOG")"

if [[ ! -f "$GLOSSARY" ]]; then exit 0; fi
cd "$PROJECT_DIR" 2>/dev/null || exit 0

# Find changed Python files under domain/
CHANGED="$(git diff --name-only HEAD 2>/dev/null | grep 'services/api/src/altune/domain/.*\.py$' || true)"
if [[ -z "$CHANGED" ]]; then exit 0; fi

NEW_TERMS=()
while IFS= read -r f; do
  [[ -f "$f" ]] || continue
  # Extract class names
  while IFS= read -r cls; do
    cls_name="$(echo "$cls" | sed -E 's/^class[[:space:]]+([A-Za-z_][A-Za-z0-9_]*).*/\1/')"
    [[ -z "$cls_name" ]] && continue
    # Skip private/internal
    [[ "$cls_name" == _* ]] && continue
    if ! grep -qi "^[[:space:]]*[-*][[:space:]]*\*\*$cls_name\*\*" "$GLOSSARY" 2>/dev/null && \
       ! grep -qi "^[[:space:]]*[-*][[:space:]]*$cls_name[[:space:]]" "$GLOSSARY" 2>/dev/null; then
      NEW_TERMS+=("$cls_name (in $f)")
    fi
  done < <(grep -E '^class [A-Z]' "$f" 2>/dev/null || true)
done <<<"$CHANGED"

if [[ ${#NEW_TERMS[@]} -eq 0 ]]; then exit 0; fi

MSG="[terminology-drift] Domain terms in code not found in docs/ubiquitous-language.md:\n"
for t in "${NEW_TERMS[@]}"; do MSG+="  - $t\n"; done
MSG+="\nAdd them to the glossary in the same commit, or confirm they're not domain terms."

echo "[terminology-drift] flagged: ${NEW_TERMS[*]}" >> "$LOG"

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
