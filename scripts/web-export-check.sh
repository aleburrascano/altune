#!/usr/bin/env bash
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
out=$(mktemp -d "${TMPDIR:-/tmp}/web-export.XXXXXX")
trap 'rm -rf "$out"' EXIT

export EXPO_PUBLIC_SUPABASE_URL=${EXPO_PUBLIC_SUPABASE_URL:-https://fixture.supabase.co}
export EXPO_PUBLIC_SUPABASE_ANON_KEY=${EXPO_PUBLIC_SUPABASE_ANON_KEY:-fixture-anon-key}
export EXPO_PUBLIC_API_URL=${EXPO_PUBLIC_API_URL:-https://api.fixture.invalid}
export EXPO_OFFLINE=${EXPO_OFFLINE:-1}
export CI=${CI:-1}

echo "web-export-check: ios and android skipped: hermesc fails with 'Exec format error' on this host's arch, so build native on EAS" >&2
echo "web-export-check: exporting web with EXPO_PUBLIC_API_URL=$EXPO_PUBLIC_API_URL into $out" >&2
if ! (cd "$root/client" && npx expo export -p web --clear --output-dir "$out" "$@"); then
  echo "web-export-check: web export failed" >&2
  exit 1
fi
[ -f "$out/index.html" ] || { echo "web-export-check: export finished but wrote no index.html" >&2; exit 1; }
echo "web-export-check: web export ok"
