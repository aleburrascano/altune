#!/usr/bin/env bash
set -euo pipefail
branch=$1
title=$2
body=$3
path=$4
cd "$WT"
if [ -z "$(git status --porcelain -- "$path")" ]; then
  echo "$path is unchanged; no PR to open"
  exit 0
fi
git add -N -- "$path"
git --no-pager diff --stat -- "$path"
{ git --no-pager diff -- "$path" || true; } | head -n 400
if [ "${OPEN_PR:-true}" != true ]; then
  echo "open-pr is off; the measured change stays in this run's log"
  exit 0
fi
git switch -q -c "$branch"
git add -- "$path"
git -c user.name=gitea-actions -c user.email=actions@altune-git.duckdns.org -c core.hooksPath=/dev/null commit -q -m "$title"
git push -q gitea "HEAD:refs/heads/$branch"
/home/ubuntu/.local/bin/forge pr create --base main --head "$branch" --title "$title" --body "$body"
