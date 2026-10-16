#!/usr/bin/env bash

set -euo pipefail

SEP=$'\035'
NL=$'\037'
BASE_REF="${NILAWAY_BASE_REF:-origin/main}"

go install go.uber.org/nilaway/cmd/nilaway@571480214735
BASE=0
if [ -n "${1:-}" ]; then
  BASE=$(tr -dc '0-9' < "$1")
fi
if ! command -v nilaway > /dev/null; then
  echo "::error::nilaway is not on PATH; add \$(go env GOPATH)/bin to PATH"
  exit 1
fi
WORK=$(mktemp -d)
BASE_TREE="$WORK/base-tree"
cleanup() {
  git worktree remove --force "$BASE_TREE" > /dev/null 2>&1 || true
  git worktree prune > /dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
REPORT="$WORK/head.txt"
PREFIX=$(git rev-parse --show-prefix 2> /dev/null || true)

scan() {
  GOMEMLIMIT="${GOMEMLIMIT:-4GiB}" nilaway $(go list ./... | grep -v /node_modules/)
}

strip_ansi() {
  sed 's/\x1b\[[0-9;]*m//g' "$1"
}

plain_text() {
  sed 's/\.go:/.go :/g' <<< "$1"
}

count_findings() {
  strip_ansi "$1" | grep -c 'Potential nil panic detected' || true
}

finding_blocks() {
  strip_ansi "$1" | awk -v root="$2" '
    function flush() {
      if (blk != "") {
        key = blk
        gsub(/:[0-9]+(:[0-9]+)?/, "", key)
        gsub(/[ \t]+/, " ", key)
        print key "\035" blk
      }
      blk = ""
    }
    /Potential nil panic detected/ { flush(); started = 1 }
    started {
      line = $0
      while ((i = index(line, root)) > 0) line = substr(line, 1, i - 1) substr(line, i + length(root))
      blk = (blk == "") ? line : blk "\037" line
    }
    END { flush() }
  '
}

block_location() {
  local first=${1%%"$NL"*}
  grep -oE -m1 '[^ :]+\.go:[0-9]+' <<< "$first" || true
}

print_block() {
  local block=$1 loc first
  plain_text "${block//"$NL"/$'\n'}"
  first=${block%%"$NL"*}
  loc=$(block_location "$block")
  if [ -n "$loc" ]; then
    echo "::error file=${PREFIX}${loc%:*},line=${loc##*:}::${first//%/%25}"
  else
    echo "::error::${first//%/%25}"
  fi
}

new_blocks() {
  awk -F "$SEP" '
    NR == FNR { seen[$1]++; next }
    seen[$1] > 0 { seen[$1]--; next }
    { print }
  ' "$1" "$2"
}

print_blocks() {
  local line
  while IFS= read -r line; do
    print_block "${line#*"$SEP"}"
  done < "$1"
}

print_blocks_in_changed_files() {
  local head_blocks=$1 changed=$2 line block loc
  while IFS= read -r line; do
    block=${line#*"$SEP"}
    loc=$(block_location "$block")
    if [ -n "$loc" ] && grep -qxF "${PREFIX}${loc%:*}" "$changed"; then
      print_block "$block"
    fi
  done < "$head_blocks"
}

scan_merge_base() {
  local merge_base=$1 out=$2 status=0
  git worktree add --detach "$BASE_TREE" "$merge_base" > /dev/null 2>&1 || return 1
  (cd "$BASE_TREE/$PREFIX" && scan) > "$out" 2>&1 || status=$?
  [ "$status" -eq 0 ] || [ "$(count_findings "$out")" -gt 0 ]
}

explain_rise() {
  local head_blocks="$WORK/head.blocks" base_blocks="$WORK/base.blocks" merge_base
  finding_blocks "$REPORT" "$(pwd -P)/" > "$head_blocks"
  if ! merge_base=$(git merge-base HEAD "$BASE_REF" 2> /dev/null); then
    echo "::warning::no merge base with ${BASE_REF}; the full finding list is in the nilaway findings group above"
    return 0
  fi
  if scan_merge_base "$merge_base" "$WORK/base.txt"; then
    finding_blocks "$WORK/base.txt" "$(cd "$BASE_TREE/$PREFIX" && pwd -P)/" > "$base_blocks"
    new_blocks "$base_blocks" "$head_blocks" > "$WORK/new.blocks"
    if [ ! -s "$WORK/new.blocks" ]; then
      echo "::warning::no new nilaway findings versus merge base ${merge_base:0:12}, so the ceiling is stale: base has $(count_findings "$WORK/base.txt") findings; lower the ceiling to that"
      return 0
    fi
    echo "New nilaway findings since merge base ${merge_base:0:12}:"
    print_blocks "$WORK/new.blocks"
    return 0
  fi
  echo "::warning::nilaway failed on merge base ${merge_base:0:12}; showing findings in files this PR changed"
  git diff --name-only "${merge_base}...HEAD" > "$WORK/changed.txt"
  print_blocks_in_changed_files "$head_blocks" "$WORK/changed.txt"
}

STATUS=0
scan > "$REPORT" 2>&1 || STATUS=$?
COUNT=$(count_findings "$REPORT")
if [ "$STATUS" -ne 0 ] && [ "$COUNT" -eq 0 ]; then
  echo "::error::nilaway exited ${STATUS} without reporting findings"
  cat "$REPORT"
  exit 1
fi
echo "::group::nilaway findings"
plain_text "$(strip_ansi "$REPORT")"
echo "::endgroup::"
echo "nilaway findings: ${COUNT} (ceiling ${BASE})"
if [ "$COUNT" -gt "$BASE" ]; then
  echo "::error::nilaway findings rose from ${BASE} to ${COUNT} — a new nil-dereference risk was introduced"
  explain_rise
  exit 1
fi
