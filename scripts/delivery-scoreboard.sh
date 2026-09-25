#!/usr/bin/env bash
# Delivery scoreboard for the software factory.
# Reads GitHub via gh. Every speed metric is paired with a quality metric,
# because AI lifts throughput while hurting stability.
#
# Local:  scripts/delivery-scoreboard.sh [owner/repo] [days]
# CI:     set GH_TOKEN + DAYS; writes to $GITHUB_STEP_SUMMARY.
# Defaults: repo = $GITHUB_REPOSITORY or aleburrascano/altune, days = $DAYS or 14.

set -euo pipefail
R="${1:-${GITHUB_REPOSITORY:-aleburrascano/altune}}"
DAYS="${2:-${DAYS:-14}}"
SINCE=$(date -u -d "-${DAYS} days" +%Y-%m-%d)
OUT="${GITHUB_STEP_SUMMARY:-/dev/stdout}"

count() { gh api -X GET search/issues -f q="repo:$R $1" --jq .total_count 2>/dev/null || echo 0; }

# merged PRs: true count + a 500-row sample for medians
merged=$(count "is:pr is:merged merged:>=$SINCE")
PRS=$(gh pr list -R "$R" --state merged --search "merged:>=$SINCE" --limit 500 \
  --json title,createdAt,mergedAt,labels 2>/dev/null || echo '[]')
sample=$(jq 'length' <<<"$PRS")

read -r cyc_med cyc_p75 < <(jq -r '
  [ .[] | ((.mergedAt|fromdateiso8601) - (.createdAt|fromdateiso8601))/60 ] | sort as $s
  | if ($s|length)==0 then "0 0"
    else "\($s[($s|length)/2|floor]|floor) \($s[($s|length)*3/4|floor]|floor)" end' <<<"$PRS")

cfr_n=$(jq '[.[] | select((.title|test("revert|rollback|hotfix";"i")) or (any(.labels[].name; test("revert|rollback|hotfix|incident";"i"))))] | length' <<<"$PRS")
cfr=$([ "$sample" -gt 0 ] && awk "BEGIN{printf \"%.1f\", 100*$cfr_n/$sample}" || echo 0)
types=$(jq -r '[.[].title | (try (capture("^(?<t>[a-z]+)").t) catch "other")] | group_by(.) | map({t:.[0], n:length}) | sort_by(-.n) | map("\(.t)=\(.n)") | join("  ")' <<<"$PRS")

# quality side
bugs=$(count "is:issue label:bug created:>=$SINCE")
c_review=$(count "is:issue label:bug label:\"caught:review\" created:>=$SINCE")
c_qa=$(count "is:issue label:bug label:\"caught:qa\" created:>=$SINCE")
c_prod=$(count "is:issue label:bug label:\"caught:prod\" created:>=$SINCE")
tagged=$((c_review + c_qa + c_prod))
untagged=$((bugs - tagged)); [ "$untagged" -lt 0 ] && untagged=0
escaped=$([ "$tagged" -gt 0 ] && awk "BEGIN{printf \"%.0f\", 100*$c_prod/$tagged}" || echo "n/a")

rel=$(gh api "repos/$R/releases" --paginate 2>/dev/null | jq --arg s "${SINCE}T00:00:00Z" '[.[]|select(.published_at>=$s)]|length' 2>/dev/null || echo 0)
perday=$([ "$DAYS" -gt 0 ] && awk "BEGIN{printf \"%.1f\", $merged/$DAYS}" || echo 0)

# local telemetry (skipped in CI)
TEL="$HOME/.claude/telemetry"; loops="n/a"
if ls "$TEL"/*.jsonl >/dev/null 2>&1; then
  loops=$(jq -s 'group_by(.session_id + "|" + .tool + "|" + (.target//"")) | map(select(length>2)) | length' "$TEL"/*.jsonl 2>/dev/null || echo 0)
fi

{
echo "# Delivery scoreboard — $R"
echo
echo "Window: last $DAYS days (since $SINCE). Generated $(date -u +%Y-%m-%dT%H:%MZ)."
echo
echo "| metric | value | quality pair |"
echo "|---|---|---|"
echo "| PRs merged | $merged (~$perday/day) | change-failure ${cfr}% ($cfr_n of $sample sampled) |"
echo "| Median PR cycle time | ${cyc_med} min (p75 ${cyc_p75}) | bug issues opened: $bugs |"
echo "| Releases (deploys) | $rel | escaped-defect rate: ${escaped}% |"
echo "| Retry-loop sessions | $loops | — |"
echo
echo "**Where bugs were caught** (of $bugs opened): review $c_review · qa $c_qa · prod $c_prod · untagged $untagged"
echo
echo "Work-type split: $types"
echo
if [ "$untagged" -gt "$tagged" ]; then
  echo "> Escaped-defect rate is only trustworthy once bugs carry a \`caught:\` label. Most are still untagged; qa/review apply them going forward."
fi
} > "$OUT"

[ "$OUT" != "/dev/stdout" ] && cat "$OUT" || true
