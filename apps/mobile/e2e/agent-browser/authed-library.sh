#!/usr/bin/env bash
#
# NON-PRODUCTION agent-browser harness: log in as the dedicated test user via
# go-api's /test/login and drive ONE authenticated screen (the library) on Expo
# web, headlessly. See docs/webauth-testing-design.md.
#
# How it works: the app mounts <TestAuthBridge/> (src/features/auth/ui), which —
# only when EXPO_PUBLIC_TEST_AUTH=1 in a dev build — calls bootstrapTestAuth()
# (src/shared/auth/testAuth.ts). That fetches a token from ${EXPO_PUBLIC_API_URL}
# /test/login and injects a session into the Supabase client's storage, so
# useSession() reports signed-in and the AuthGate renders authed screens instead
# of redirecting to /sign-in. This script just boots the web bundle with that
# flag set and drives the browser.
#
# Prerequisites:
#   - go-api running with a non-prod ENV (so /test/login is mounted), reachable
#     at $EXPO_PUBLIC_API_URL. `npm run dev:up` at the repo root starts it.
#   - agent-browser installed (`npm i -g agent-browser && agent-browser install`).
#   - Node 22 (the Supabase client needs a global WebSocket).
#
# Usage:
#   EXPO_PUBLIC_API_URL=http://127.0.0.1:8000 \
#   EXPO_PUBLIC_SUPABASE_URL=https://<ref>.supabase.co \
#   EXPO_PUBLIC_SUPABASE_ANON_KEY=<anon> \
#   ./e2e/agent-browser/authed-library.sh
set -euo pipefail

PORT="${EXPO_WEB_PORT:-8081}"
APP_URL="http://localhost:${PORT}"
OUT_DIR="${OUT_DIR:-/tmp/altune-authed-harness}"
mkdir -p "$OUT_DIR"

: "${EXPO_PUBLIC_API_URL:?set EXPO_PUBLIC_API_URL to the running go-api base URL}"
: "${EXPO_PUBLIC_SUPABASE_URL:?set EXPO_PUBLIC_SUPABASE_URL}"
: "${EXPO_PUBLIC_SUPABASE_ANON_KEY:?set EXPO_PUBLIC_SUPABASE_ANON_KEY}"

# The one guard that turns the client-side test-auth path on. A production build
# has __DEV__ === false, so this flag is inert there.
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
# Give the bundle time to boot, run TestAuthBridge -> /test/login, inject the
# session, and re-render past the AuthGate.
sleep 8
agent-browser open "${APP_URL}/library"
sleep 4

echo "==> capturing state"
agent-browser get url            | tee "$OUT_DIR/url.txt"
agent-browser snapshot           | tee "$OUT_DIR/snapshot.txt"
agent-browser screenshot --output "$OUT_DIR/library.png" || true

# The proof: we are on the library route, NOT bounced to /sign-in.
if agent-browser get url | grep -q "sign-in"; then
  echo "FAIL: redirected to sign-in — test-auth session was not injected"
  exit 1
fi
echo "PASS: authenticated library screen rendered — artifacts in $OUT_DIR"
