#!/usr/bin/env bash
set -uo pipefail

src=services/go-api

probes=$(git grep -nE '\.\((interface ?\{|[a-z][a-z0-9]*\.[A-Z][A-Za-z0-9]*\))' -- "$src/*.go" ":(exclude)*_test.go" \
  | grep -vE 'http\.Flusher|events\.Subjected|service\.TapEvent|ast\.[A-Z]|\.Value\(' || true)

if [ -n "$probes" ]; then
  echo "capability-probes: a type assertion probes for an optional capability; require it on the port and wire a real or named absent adapter instead:" >&2
  sed 's/^/  /' <<<"$probes" >&2
  exit 1
fi
