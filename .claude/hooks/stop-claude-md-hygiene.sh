#!/usr/bin/env bash
# stop-claude-md-hygiene: blocks Stop if any touched feature/context dir is missing
# or has a CLAUDE.md older than its source files. See plan:
# C:\Users\Alessandro\.claude\plans\hey-so-in-the-twinkly-nebula.md
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"
LOG="$PROJECT_DIR/.claude/hooks.log"
DRIFT_LOG="$PROJECT_DIR/.claude/claude-md-drift.log"
mkdir -p "$(dirname "$LOG")"

cd "$PROJECT_DIR" 2>/dev/null || exit 0

# Bail if not a git repo (nothing to diff)
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0

# Override: latest commit body contains [ALLOW-CLAUDE-MD-DRIFT: ...]
LAST_MSG="$(git log -1 --pretty=%B 2>/dev/null || echo '')"
if [[ "$LAST_MSG" =~ \[ALLOW-CLAUDE-MD-DRIFT: ]]; then
  echo "[claude-md-drift] OVERRIDE on $(git rev-parse --short HEAD 2>/dev/null || echo '?'): $(echo "$LAST_MSG" | head -1)" >> "$DRIFT_LOG"
  exit 0
fi

# ---------------------------------------------------------------------------
# Collect touched files: working tree + last 10 commits + (if feature branch)
# branch diff against main/master.
# ---------------------------------------------------------------------------
TOUCHED_FILES=""
TOUCHED_FILES+="$(git status --porcelain 2>/dev/null | awk '{print $NF}')"$'\n'
TOUCHED_FILES+="$(git log -10 --name-only --pretty=format: 2>/dev/null)"$'\n'

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '')"
if [[ "$CURRENT_BRANCH" != "main" && "$CURRENT_BRANCH" != "master" && -n "$CURRENT_BRANCH" ]]; then
  for BASE_REF in main master; do
    BASE="$(git merge-base HEAD "$BASE_REF" 2>/dev/null || echo '')"
    if [[ -n "$BASE" ]]; then
      TOUCHED_FILES+="$(git diff --name-only "$BASE"...HEAD 2>/dev/null)"$'\n'
      break
    fi
  done
fi

# ---------------------------------------------------------------------------
# Reduce to unique target dirs under the two tracked roots.
#   apps/mobile/src/features/<feat>/              (exclude _template)
#   services/api/src/altune/<layer>/<context>/    (5 layers)
# ---------------------------------------------------------------------------
declare -A SEEN_DIRS=()
TARGET_DIRS=()

add_dir() {
  local d="$1"
  if [[ -z "${SEEN_DIRS[$d]:-}" ]]; then
    SEEN_DIRS[$d]=1
    TARGET_DIRS+=("$d")
  fi
}

while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  if [[ "$f" =~ ^(apps/mobile/src/features/[^_/][^/]*)/ ]]; then
    add_dir "${BASH_REMATCH[1]}"
  elif [[ "$f" =~ ^(services/api/src/altune/(domain|application)/[^/]+)/ ]]; then
    add_dir "${BASH_REMATCH[1]}"
  elif [[ "$f" =~ ^(services/api/src/altune/adapters/inbound/http/[^/]+)/ ]]; then
    add_dir "${BASH_REMATCH[1]}"
  elif [[ "$f" =~ ^(services/api/src/altune/adapters/outbound/persistence/[^/]+)/ ]]; then
    add_dir "${BASH_REMATCH[1]}"
  elif [[ "$f" =~ ^(services/api/src/altune/adapters/outbound/[^/]+)/ ]]; then
    # Skip the persistence subtree (already handled above by the more specific match).
    seg="${BASH_REMATCH[1]##*/}"
    [[ "$seg" == "persistence" ]] && continue
    add_dir "${BASH_REMATCH[1]}"
  fi
done <<<"$TOUCHED_FILES"

[[ ${#TARGET_DIRS[@]} -eq 0 ]] && exit 0

# ---------------------------------------------------------------------------
# Classify each dir: MISSING (no CLAUDE.md) or STALE (CLAUDE.md older than code).
# Skip dirs with <3 source files (matches the skill's noise threshold).
# ---------------------------------------------------------------------------
MISSING=()
STALE=()

for dir in "${TARGET_DIRS[@]}"; do
  [[ -d "$dir" ]] || continue

  src_count=$(find "$dir" -maxdepth 3 -type f \( -name '*.py' -o -name '*.ts' -o -name '*.tsx' \) 2>/dev/null | wc -l | tr -d ' ')
  [[ "$src_count" -lt 3 ]] && continue

  if [[ ! -f "$dir/CLAUDE.md" ]]; then
    MISSING+=("$dir")
    continue
  fi

  code_ts="$(git log -1 --format=%ct -- "$dir" ":(exclude)$dir/CLAUDE.md" 2>/dev/null || echo '0')"
  claude_ts="$(git log -1 --format=%ct -- "$dir/CLAUDE.md" 2>/dev/null || echo '0')"
  code_ts="${code_ts:-0}"
  claude_ts="${claude_ts:-0}"

  if [[ "$code_ts" -gt "$claude_ts" ]]; then
    STALE+=("$dir")
  fi
done

if [[ ${#MISSING[@]} -eq 0 && ${#STALE[@]} -eq 0 ]]; then exit 0; fi

# ---------------------------------------------------------------------------
# Build block message + emit decision:block JSON.
# ---------------------------------------------------------------------------
MSG="CLAUDE.md hygiene gap detected. Cannot end session."$'\n\n'

if [[ ${#MISSING[@]} -gt 0 ]]; then
  MSG+="Missing CLAUDE.md (run /update-nested-claude-md <dir> for each):"$'\n'
  for d in "${MISSING[@]}"; do MSG+="  - $d/"$'\n'; done
  MSG+=$'\n'
fi

if [[ ${#STALE[@]} -gt 0 ]]; then
  MSG+="Stale CLAUDE.md (source files touched more recently than CLAUDE.md):"$'\n'
  for d in "${STALE[@]}"; do MSG+="  - $d/"$'\n'; done
  MSG+=$'\n'
fi

MSG+="After fixing, commit (\`docs(claude-md): ...\`), then end the session again."$'\n'
MSG+="Bypass: add \`[ALLOW-CLAUDE-MD-DRIFT: <reason>]\` to the most recent commit body."

echo "[claude-md-drift] block at $(git rev-parse --short HEAD 2>/dev/null || echo '?'): missing=${#MISSING[@]} stale=${#STALE[@]}" >> "$DRIFT_LOG"

python - "$MSG" <<'PYEOF'
import json, sys
print(json.dumps({"decision": "block", "reason": sys.argv[1]}))
PYEOF

exit 0
