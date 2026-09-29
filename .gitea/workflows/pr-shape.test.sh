#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/pr-shape.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

cat > "$work/forge" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
  "pr view") jq -n --arg b "$PR_BODY" '{body:$b,additions:10,deletions:0,files:[{path:"README.md"}]}' ;;
  "issue view") [ -n "${ISSUE_FAIL:-}" ] && exit 1; jq -n --arg b "$ISSUE_BODY" --arg l "$ISSUE_LABELS" '{body:$b,labels:($l|split(",")|map(select(.!="")|{name:.}))}' ;;
esac
STUB
chmod +x "$work/forge"

withDone=$'## Goal\nx\n\n## Done when\n- [ ] it works\n\n## Verify\ny'
noDone=$'## Goal\nx\n\n## Done when\nnothing yet\n\n## Verify\n- not a bullet of done-when'
longBody=$'This PR explains the change thoroughly.\nCloses #7'

t() {
  local want=$1 desc=$2 pat=$3
  out=$(FORGE="$work/forge" bash "$script" 1 2>&1); got=$?
  [ "$got" = "$want" ] || { echo "FAIL want exit $want got $got: $desc"; echo "$out" | sed 's/^/    /'; fail=1; return; }
  if [ -n "$pat" ]; then
    grep -qE "$pat" <<<"$out" || { echo "FAIL output lacks /$pat/: $desc"; echo "$out" | sed 's/^/    /'; fail=1; }
  fi
}

export ISSUE_BODY=$noDone ISSUE_LABELS="" ISSUE_FAIL="" PR_BODY=$longBody
t 0 "no labels, no bullets" "pr-shape: 0 note"
ISSUE_LABELS="risk"
t 1 "risk without Coverage" "::error::.*Coverage"
ISSUE_LABELS="complexity:deep"
t 1 "complexity:deep without Coverage" "::error::.*Coverage"
PR_BODY=$'This PR explains the change thoroughly.\nCloses #7\n\n## Coverage\nall'
t 0 "Coverage present" "pr-shape: 0 note"
ISSUE_LABELS="band:now"
PR_BODY=$longBody
t 0 "unrelated label" "pr-shape: 0 note"

ISSUE_BODY=$withDone ISSUE_LABELS=""
t 1 "Done-when bullet without DONE-WHEN line" "::error::.*DONE-WHEN"
PR_BODY=$'This PR explains the change thoroughly.\nCloses #7\nDONE-WHEN: it works → a::b'
t 0 "DONE-WHEN line present" "pr-shape: 0 note"
PR_BODY=$longBody ISSUE_BODY=$noDone
t 0 "no Done-when bullet" "pr-shape: 0 note"

ISSUE_LABELS="risk" ISSUE_BODY=$withDone PR_BODY=$'This PR explains the change thoroughly.\nFixes #7'
t 1 "both missing reports an error" "pr-shape: 0 note"

PR_BODY=$'A body long enough to pass the length check.'
t 0 "no Closes link warns and passes" "::warning::.*Closes"
PR_BODY=$longBody ISSUE_FAIL=1
t 0 "issue read failure warns and passes" "::warning::.*could not read issue"
ISSUE_FAIL=""

PR_BODY=$'short\nCloses #7' ISSUE_LABELS="" ISSUE_BODY=$noDone
t 0 "existing short-body warning still passes" "::warning::.*little or no description"

[ "$fail" = 0 ] && echo "pr-shape tests ok"
exit $fail
