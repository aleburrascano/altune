#!/usr/bin/env bash
set -uo pipefail

: "${GITEA_API:?}" "${GITEA_TOKEN:?}" "${REPO:?}" "${MIRROR_KEY:?}" "${GITHUB_REMOTE:?}"
BRIDGE_GIT=${BRIDGE_GIT:-$HOME/.cache/altune-github-bridge.git}
CONTEXT="github / pr-checks"
GITEA_GIT="${GITEA_API%/api/v1}/$REPO.git"

gitea() {
  curl -fsS -K - "$@" <<<"header = \"Authorization: token $GITEA_TOKEN\""
}

bridge_git() {
  git -C "$BRIDGE_GIT" "$@"
}

github_git() {
  GIT_SSH_COMMAND="ssh -i $MIRROR_KEY -o IdentitiesOnly=yes" bridge_git "$@"
}

open_prs() {
  local page=1 body count
  while :; do
    body=$(gitea "$GITEA_API/repos/$REPO/pulls?state=open&limit=50&page=$page") || return 1
    count=$(jq 'length' <<<"$body") || return 1
    [ "$count" -gt 0 ] || return 0
    jq -r '.[].number' <<<"$body"
    page=$((page + 1))
  done
}

fetch_head() {
  GIT_TERMINAL_PROMPT=0 GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.extraHeader \
    GIT_CONFIG_VALUE_0="Authorization: token $GITEA_TOKEN" \
    bridge_git fetch -q "$GITEA_GIT" "+refs/pull/$1/head:refs/pull/$1/head" &&
    bridge_git rev-parse "refs/pull/$1/head"
}

check_run() {
  gh api "repos/$REPO/commits/$1/check-runs?per_page=100" |
    jq -c '[.check_runs[] | select(.name == "pr-checks")] | sort_by(.id) | last // {}'
}

to_state() {
  jq -r '
    if .status != "completed" then "pending"
    elif (.conclusion | IN("success", "skipped", "neutral")) then "success"
    else "failure" end' <<<"$1"
}

describe() {
  case $1 in
    success) echo "pr-checks passed on github" ;;
    failure) echo "pr-checks failed on github" ;;
    *) echo "pr-checks pending on github" ;;
  esac
}

latest_state() {
  local body
  body=$(gitea "$GITEA_API/repos/$REPO/commits/$1/statuses?limit=50") || return 1
  jq -r --arg c "$CONTEXT" '[.[] | select(.context == $c)] | sort_by(.id) | last | .status // .state // ""' <<<"$body"
}

post_status() {
  local sha=$1 state=$2 url=$3 payload
  payload=$(jq -nc --arg s "$state" --arg c "$CONTEXT" --arg u "$url" --arg d "$(describe "$state")" \
    '{state: $s, context: $c, description: $d} + (if $u == "" then {} else {target_url: $u} end)')
  gitea -X POST -H 'Content-Type: application/json' -d "$payload" "$GITEA_API/repos/$REPO/statuses/$sha" >/dev/null
}

bridge_pr() {
  local n=$1 sha run state url last
  sha=$(fetch_head "$n") || { echo "pr $n: fetch failed" >&2; return 1; }
  if [ "${remote[$n]:-}" != "$sha" ]; then
    github_git push -q --force "$GITHUB_REMOTE" "$sha:refs/heads/pr/$n" || { echo "pr $n: push failed" >&2; return 1; }
    echo "pr $n: pushed ${sha:0:12}"
  fi
  run=$(check_run "$sha") || { echo "pr $n: check-runs failed" >&2; return 1; }
  state=$(to_state "$run") || { echo "pr $n: unreadable check run" >&2; return 1; }
  url=$(jq -r '.html_url // ""' <<<"$run")
  last=$(latest_state "$sha") || { echo "pr $n: statuses failed" >&2; return 1; }
  [ "$last" = "$state" ] && return 0
  post_status "$sha" "$state" "$url" || { echo "pr $n: status post failed" >&2; return 1; }
  echo "pr $n: $state"
}

[ -e "$BRIDGE_GIT/HEAD" ] || git init -q --bare "$BRIDGE_GIT" || exit 1

prs=$(open_prs) || { echo "listing gitea pull requests failed" >&2; exit 1; }
listing=$(github_git ls-remote "$GITHUB_REMOTE" 'refs/heads/pr/*') || { echo "listing github branches failed" >&2; exit 1; }

declare -A remote open
while read -r sha ref; do
  num=${ref#refs/heads/pr/}
  [[ $num =~ ^[0-9]+$ ]] && remote[$num]=$sha
done <<<"$listing"

total=0
failed=0
for n in $prs; do
  open[$n]=1
  total=$((total + 1))
  bridge_pr "$n" || failed=$((failed + 1))
done

for n in "${!remote[@]}"; do
  [ -n "${open[$n]:-}" ] && continue
  github_git push -q "$GITHUB_REMOTE" --delete "refs/heads/pr/$n" && echo "pr $n: deleted github branch" ||
    echo "pr $n: branch delete failed" >&2
done

[ "$total" -eq 0 ] || [ "$failed" -lt "$total" ]
