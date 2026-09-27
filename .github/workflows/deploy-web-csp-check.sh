#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
caddyfile=$root/services/go-api/deploy/Caddyfile
dist=$root/apps/mobile/dist
blocked=0
while IFS= read -r inline_script; do
  [ -z "$inline_script" ] && continue
  digest=$(printf '%s' "$inline_script" | openssl dgst -sha256 -binary | base64)
  if ! grep -qF "'sha256-$digest'" "$caddyfile"; then
    echo "::error title=Inline script blocked by CSP::add 'sha256-$digest' to script-src in services/go-api/deploy/Caddyfile for: $inline_script"
    blocked=1
  fi
done < <(find "$dist" -name '*.html' -exec cat {} + \
  | grep -o '<script[^>]*>[^<][^<]*</script>' \
  | sed -e 's/^<script[^>]*>//' -e 's#</script>$##' | sort -u)
exit "$blocked"
