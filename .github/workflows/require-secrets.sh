#!/usr/bin/env bash

# Fail the job with one ::error:: naming every required secret that is unset.
# Usage: bash .github/workflows/require-secrets.sh NAME...   (values come from env)

missing=""
for name in "$@"; do
  [ -z "${!name:-}" ] && missing="$missing $name"
done
if [ -n "$missing" ]; then
  echo "::error title=Missing secrets::Set these repo secrets:$missing"
  exit 1
fi
