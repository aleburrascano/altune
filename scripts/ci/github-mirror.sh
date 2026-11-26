#!/usr/bin/env bash
set -euo pipefail

: "${MIRROR_KEY:?}" "${GITHUB_REMOTE:?}"
BRIDGE_GIT=${BRIDGE_GIT:-$HOME/.cache/altune-github-bridge.git}

sha=$(git --git-dir "$BRIDGE_GIT" rev-parse --verify refs/heads/main)

GIT_SSH_COMMAND="ssh -i $MIRROR_KEY -o IdentitiesOnly=yes" \
  git --git-dir "$BRIDGE_GIT" push --force "$GITHUB_REMOTE" refs/heads/main:refs/heads/main

echo "github main is ${sha:0:12}"
