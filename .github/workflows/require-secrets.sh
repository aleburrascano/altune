#!/usr/bin/env bash


missing=""
for name in "$@"; do
  [ -z "${!name:-}" ] && missing="$missing $name"
done
if [ -n "$missing" ]; then
  echo "::error title=Missing secrets::Set these repo secrets:$missing"
  exit 1
fi
