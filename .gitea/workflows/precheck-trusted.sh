#!/usr/bin/env bash
set -uo pipefail
head=$(git rev-parse --verify "$1^{commit}") || exit 1
base_ref=$2
forge=${FORGE:-$(command -v forge || echo /home/ubuntu/.local/bin/forge)}
base=$(git merge-base "$base_ref" "$head") || exit 1
statuses=$("$forge" api "repos/{owner}/{repo}/commits/$head/statuses") || exit 1
jq -e --arg want "against $base" '
  [.[] | select(.context == "harness-precheck" and .creator.login == "lane-bot")]
  | sort_by(.id) | last
  | .status == "success" and (.description | endswith($want))' <<<"$statuses" >/dev/null
