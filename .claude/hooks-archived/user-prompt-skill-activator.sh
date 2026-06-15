#!/usr/bin/env bash
# user-prompt-skill-activator: reads .claude/skill-rules.json and injects skill suggestions
# based on prompt content and cwd. Uses Python for JSON + regex parsing.
set -euo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"
RULES_FILE="$PROJECT_DIR/.claude/skill-rules.json"
LOG="$PROJECT_DIR/.claude/hooks.log"
mkdir -p "$(dirname "$LOG")"

if [[ ! -f "$RULES_FILE" ]]; then exit 0; fi

PAYLOAD="$(cat || true)"

python - <<'PYEOF' <<<"$PAYLOAD"
import json, re, sys, os, pathlib

payload_raw = sys.stdin.read() if not sys.stdin.isatty() else ""
try:
    payload = json.loads(payload_raw) if payload_raw else {}
except Exception:
    payload = {}

prompt = payload.get("prompt") or payload.get("user_prompt") or ""
cwd = payload.get("cwd") or os.getcwd()

# --skip toggle to suppress
if "--skip" in prompt.lower() or "[skip-skill-activator]" in prompt.lower():
    sys.exit(0)

rules_path = pathlib.Path(os.environ.get("CLAUDE_PROJECT_DIR", ".")) / ".claude" / "skill-rules.json"
if not rules_path.exists():
    sys.exit(0)

try:
    rules = json.loads(rules_path.read_text(encoding="utf-8")).get("rules", [])
except Exception:
    sys.exit(0)

suggestions = []
for rule in rules:
    matched = False
    if "if_prompt_matches" in rule and re.search(rule["if_prompt_matches"], prompt):
        matched = True
    if "if_cwd_matches" in rule and re.search(rule["if_cwd_matches"], cwd):
        matched = True
    if matched:
        skills = rule.get("suggest_skills") or ([rule["suggest"]] if "suggest" in rule else [])
        reason = rule.get("reason", "")
        for s in skills:
            suggestions.append((s, reason))

if not suggestions:
    sys.exit(0)

# Dedupe by skill name (keep first reason)
seen = {}
for s, r in suggestions:
    if s not in seen:
        seen[s] = r

lines = ["[skill-activator] Applicable skills for this turn:"]
for s, r in seen.items():
    lines.append(f"  - /{s} — {r}")
lines.append("(append `--skip` to suppress this activation)")
msg = "\n".join(lines)

out = {
    "hookSpecificOutput": {
        "hookEventName": "UserPromptSubmit",
        "additionalContext": msg
    }
}
print(json.dumps(out))
PYEOF

exit 0
