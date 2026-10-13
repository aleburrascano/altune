#!/usr/bin/env bash

set -euo pipefail

if [ "$EVENT" = "workflow_dispatch" ]; then
  V="$INPUT_VERSION"
else
  V="${GITHUB_REF_NAME#v}"
fi
echo "version=$V" >> "$GITHUB_OUTPUT"
echo "Building version $V"
