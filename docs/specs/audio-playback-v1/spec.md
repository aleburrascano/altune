# Audio Playback v1

> Spec for `audio-playback-v1` — version 1, drafted 2026-06-11.
> Authors: solo + Claude.
> Status: Draft.

## Problem

Users can save tracks to their library and acquire audio files, but there is no way to play them. The acquired MP3s sit on the server with no streaming endpoint and no player UI. The predecessor project (music-manager) had a Flask-based streaming approach that broke when the HTTP connection dropped mid-playback — it lacked Range request support, so seeks and reconnects were impossible.

## User value

The user can tap a track in their library and hear it play. Playback continues across screen navigation, app backgrounding, and lock screen. Seeking (scrubbing) works instantly. The old connection-drop problem is eliminated by proper HTTP Range support.

## Scope tier / MVP cut

- **Minimal (ship this):** Single-track playback with play/pause/seek. Backend streaming endpoint with Range support. Mini-player bar + full-screen player modal. Background audio. Lock screen controls (play/pause only).
- **Deferred to post-launch:** Queue/playlist playback, next/previous, shuffle, repeat, offline caching, gapless playback, crossfade, equalizer, playback history/analytics, CDN/edge caching.
- **Justified exceptions:** HTTP Range support (needed now because the predecessor project's lack of Range support was the root cause of the streaming failure — this is the core fix, not a scale concern).

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

### Backend — audio streaming endpoint

1. **AC#1** — Given an authenticated user who owns a track with `acquisition_status == READY`, when they request `GET /v1/tracks/{track_id}/audio`, then the response is `200 OK` with `Content-Type: audio/mpeg`, `Accept-Ranges: bytes`, `Content-Length` matching the file size, and the body is the MP3 file content.

2. **AC#2** — Given a request with `Range: bytes=500000-` header, when the backend serves the audio, then the response is `206 Partial Content` with `Content-Range: bytes 500000-{end}/{total}` and only the requested byte range in the body.

3. **AC#3** — Given a track the user does not own, when they request `GET /v1/tracks/{track_id}/audio`, then the response is `404 Not Found` (do not reveal track existence to other users).

4. **AC#4** — Given a track with `acquisition_status != READY` (pending or failed), when the user requests audio, then the response is `404 Not Found`.

5. **AC#5** — Given a track where `audio_ref` points to a file that no longer exists on disk, when the user requests audio, then the response is `404 Not Found` and a `warning` log is emitted (`audio_file_missing`).

6. **AC#6** — Given an unauthenticated request (no Bearer token), when requesting any audio endpoint, then the response is `401 Unauthorized`.

7. **AC#7** — The response includes `Cache-Control: private, max-age=86400` (audio content is immutable per track, user-scoped).

### Mobile — playback provider

8. **AC#8** — Given the app is launched, when the PlaybackProvider mounts, then `expo-av` audio mode is configured with `staysActiveInBackground: true` and `playsInSilentModeIOS: true`.

9. **AC#9** — Given a track with `acquisition_status == "ready"`, when the user taps play, then `Audio.Sound` loads the URL `{API_BASE}/v1/tracks/{trackId}/audio` with the Bearer token in request headers, and playback begins within 3 seconds on a normal connection.

10. **AC#10** — Given playback is active, when the user navigates to a different tab or screen, then playback continues uninterrupted (PlaybackProvider persists in root layout).

11. **AC#11** — Given playback is active, when the user backgrounds the app or locks the screen, then audio continues playing.

### Mobile — mini-player

12. **AC#12** — Given a track is loaded (playing or paused), then a mini-player bar is visible above the tab bar showing: artwork thumbnail (or placeholder), track title, artist name, play/pause button, and a slim progress bar.

13. **AC#13** — Given no track is loaded, then the mini-player is not rendered and the tab bar sits at the bottom edge as normal.

14. **AC#14** — Given the mini-player is visible, when the user taps the play/pause button, then playback toggles between playing and paused.

15. **AC#15** — Given the mini-player is visible, when the user taps anywhere on it except the play/pause button, then the full-screen player opens as a modal.

### Mobile — full-screen player

16. **AC#16** — The full-screen player displays: large artwork (or placeholder), track title, artist name, a draggable scrubber/progress bar with elapsed and remaining time labels, and a play/pause button.

17. **AC#17** — Given the full player is open, when the user drags the scrubber to a new position, then playback seeks to that position (expo-av issues a new Range request to the backend).

18. **AC#18** — Given the full player is open, when the user taps a close/minimize control, then the modal dismisses and the mini-player remains visible.

### Mobile — error handling

19. **AC#19** — Given a track whose audio is not yet acquired (`acquisition_status != "ready"`), then the play button is disabled or hidden and a status indicator shows the track is pending/failed.

20. **AC#20** — Given playback fails (network error, 404, etc.), then the mini-player shows an error state and playback stops cleanly (no crash, no stuck spinner).

### Mobile — accessibility

21. **AC#21** — All playback controls (play/pause, scrubber, close) have accessibility labels. The mini-player announces the current track when focused by a screen reader.

## Out of scope

- Queue/playlist playback, next/previous track navigation, shuffle, repeat modes
- Offline caching / download-to-device
- Gapless playback or crossfade between tracks
- Lock screen controls beyond play/pause (skip, scrub from lock screen deferred)
- Playback history or analytics events (no `PlayStarted` / `PlayCompleted` domain events yet)
- Audio quality selection (always 320kbps MP3)
- CDN or edge caching of audio files
- Streaming from external URLs (only server-hosted acquired audio)
- Migration to `react-native-track-player` (deferred until queue features are specced)

## Design considerations

- [vault: wiki/concepts/Hexagonal Architecture.md] — the audio streaming endpoint is an inbound HTTP adapter. It reads from the filesystem via the existing `FilesystemAudioStore` (or directly, since `FileResponse` needs a path). The endpoint does not add domain logic — it's a thin auth-gated file server.
- [vault: wiki/concepts/Vertical Slice Architecture.md] — the mobile `playback` feature owns its entire slice: provider, hooks, UI components, types. No cross-feature imports.

High-level approach:

- **Backend:** This is a **read** path in the `catalog` bounded context. The endpoint resolves `track_id` → `Track` (via existing repository), checks ownership + readiness, resolves `audio_ref` to an absolute path via `MUSIC_DIR`, and returns a `FileResponse`. No new aggregate, value object, or port needed — just a new route in the catalog router.
- **Mobile:** New `features/playback/` vertical slice. The `PlaybackProvider` is a React context mounted in the root layout (shared across all screens). It wraps `expo-av`'s `Audio.Sound` API. The MiniPlayer is rendered conditionally in the tab layout. The FullPlayer is a modal screen.
- **No new external dependency on the backend.** FastAPI's `FileResponse` handles Range requests natively.
- **New mobile dependency:** `expo-av` — installed via `npx expo install expo-av`. Works in Expo Go (no dev build required). ADR-0008 compatibility preserved.

## Dependencies

- **Bounded contexts:** catalog (existing — track ownership, audio_ref resolution)
- **Other features:** acquire-track (must be shipped — provides the audio files). Library feature (provides the track list UI where play buttons appear).
- **External services:** none
- **Library/framework additions:** `expo-av` on mobile (Expo Go compatible, no dev build)

## Risks / open questions

- **Risk:** expo-av's background audio on Android may require foreground service configuration in `app.json`. Mitigation: test on a real Android device early; Expo's docs cover the `android.foregroundService` config.
- **Risk:** Bearer token in audio request headers — if the token expires mid-playback of a long track, the initial load succeeds but a seek near the end could fail with 401. Mitigation: expo-av loads the full response progressively; for a ~10MB file this completes well before token expiry. If it becomes an issue, refresh the token before seeking.
- **Open question:** Should the play button appear on tracks in the library list view, or only on the detail screen? To resolve: implement on detail screen first, add to list view if it feels natural during development.

## Telemetry

- **Log events:**
  - `audio_stream_started` — track_id, user_id, file_size_bytes (on successful FileResponse)
  - `audio_file_missing` — track_id, audio_ref, user_id (file on disk not found despite READY status)
- **Metrics:** (deferred to post-launch — no metrics infra yet)
- **Alerts:** (deferred to post-launch)

## Related

- [vault: wiki/concepts/Hexagonal Architecture.md] — endpoint placement
- [vault: wiki/concepts/Vertical Slice Architecture.md] — mobile feature structure
- Related ADRs: `docs/adr/0008-mobile-stack-expo-go.md` (expo-av compatibility)
- Predecessor feature specs: `docs/specs/acquire-track/spec.md` (provides audio files), `docs/specs/view-library/spec.md` (provides track list UI)
