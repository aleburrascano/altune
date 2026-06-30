# Phase 4 — background push on acquisition complete (implementation notes)

**Status:** Specified, not built. Deferred because it needs (a) a real device on a
dev build — push tokens don't work in Expo Go / iOS simulator, (b) a
human-reviewed schema migration (project rule: AI must not author DB schema),
and (c) backend Go that must be compiled/verified locally.

**Goal:** When a track finishes acquiring while the app is backgrounded or
closed, send the user a push notification ("'Midnight City' is ready") that
deep-links to the track.

## Client (`apps/mobile`)

1. `npx expo install expo-notifications` (+ add the `expo-notifications` config
   plugin to `app.json`; an iOS dev build is required — Expo Go can't retrieve a
   token on SDK 53+).
2. New `apps/mobile/src/shared/notifications/registerPush.ts`:
   - `Notifications.requestPermissionsAsync()` → on grant,
     `Notifications.getExpoPushTokenAsync()` → POST the token to a new
     `PUT /v1/me/push-token` endpoint (auth'd; body `{ token }`).
   - Call it once after sign-in (mount alongside `ServerEventsBridge` in
     `src/app/_layout.tsx`).
3. Notification handler: on tap, read the `track_id` from the notification data
   and `router.push` to that track's detail (reuse the detail handoff seam).
4. Set `Notifications.setNotificationHandler` (use `shouldShowBanner` /
   `shouldShowList`, not the deprecated `shouldShowAlert`).

## Backend (`services/go-api`)

1. **Migration (human-authored + reviewed):** a `push_tokens` table —
   `(user_id uuid, token text, platform text, updated_at timestamptz)`, unique on
   `(user_id, token)`. Do NOT auto-generate this; design indexes/constraints by
   hand per `.claude/rules/backend/go-database.md`.
2. `PUT /v1/me/push-token` handler → upsert the token for the authed user.
3. An `ExpoPushSender` outbound port + adapter that POSTs to
   `https://exp.host/--/api/v2/push/send` (`{ to, title, body, data:{track_id} }`),
   with the resilience baseline (timeout, bounded retry).
4. Wire it as a second consumer of the completion event: where
   `acquire.go` publishes `track_acquisition_completed`, also enqueue a push to
   the user's stored tokens. Keep it best-effort (failure must not affect
   acquisition); log on failure.

## Notes

- In-app realtime (Phases 1–3) already covers the *foreground* case; push is
  purely for backgrounded/closed apps.
- Verification requires a dev build on a physical device + a valid Expo project
  push setup (APNs key for iOS / FCM for Android).
