#!/usr/bin/env bash

set -euo pipefail

if [ "$EVENT" != "push" ] \
  || [ -z "$BEFORE" ] \
  || [ "$BEFORE" = "0000000000000000000000000000000000000000" ] \
  || ! git cat-file -e "${BEFORE}^{commit}" 2>/dev/null; then
  echo "No usable diff base ($EVENT / $BEFORE); deploying to be safe."
  echo "deploy=true" >> "$GITHUB_OUTPUT"
  exit 0
fi
echo "Changed files ${BEFORE}..${AFTER}:"
git diff --name-only "$BEFORE" "$AFTER" | sed 's/^/  /'
deploy=false
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    services/go-api/*|services/overseer/*) ;;
    *) continue ;;
  esac
  case "$f" in
    *.md) continue ;;
  esac
  deploy=true
  break
done < <(git diff --name-only "$BEFORE" "$AFTER")
echo "Deployable code changed: $deploy"
echo "deploy=$deploy" >> "$GITHUB_OUTPUT"
