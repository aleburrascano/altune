#!/usr/bin/env bash



set -euo pipefail

PORT="${EXPO_WEB_PORT:-8081}"
APP_URL="http://localhost:${PORT}"
OUT_DIR="${OUT_DIR:-/tmp/altune-authed-harness}"
mkdir -p "$OUT_DIR"

: "${EXPO_PUBLIC_API_URL:?set EXPO_PUBLIC_API_URL to the running go-api base URL}"
: "${EXPO_PUBLIC_SUPABASE_URL:?set EXPO_PUBLIC_SUPABASE_URL}"
: "${EXPO_PUBLIC_SUPABASE_ANON_KEY:?set EXPO_PUBLIC_SUPABASE_ANON_KEY}"

export EXPO_PUBLIC_TEST_AUTH=1

cd "$(dirname "$0")/../.."

echo "==> starting Expo web on :${PORT} (EXPO_PUBLIC_TEST_AUTH=1)"
npx expo start --web --port "$PORT" > "$OUT_DIR/expo.log" 2>&1 &
EXPO_PID=$!
trap 'kill "$EXPO_PID" 2>/dev/null || true' EXIT

echo "==> waiting for the bundler at ${APP_URL}"
for _ in $(seq 1 120); do
  if curl -sf "$APP_URL" > /dev/null 2>&1; then break; fi
  sleep 2
done

echo "==> opening the library screen"
agent-browser open "${APP_URL}/library"
sleep 8
agent-browser open "${APP_URL}/library"
sleep 4

echo "==> capturing state"
agent-browser get url            | tee "$OUT_DIR/url.txt"
agent-browser snapshot           | tee "$OUT_DIR/snapshot.txt"
agent-browser screenshot --output "$OUT_DIR/library.png" || true

if agent-browser get url | grep -q "sign-in"; then
  echo "FAIL: redirected to sign-in — test-auth session was not injected"
  exit 1
fi
echo "PASS: authenticated library screen rendered — artifacts in $OUT_DIR"
