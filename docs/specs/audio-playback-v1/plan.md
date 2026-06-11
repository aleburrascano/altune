# audio-playback-v1 — implementation plan

Spec: docs/specs/audio-playback-v1/spec.md

## Slices

### Slice 1: Audio streaming endpoint — happy path (AC#1, AC#7)

New `GET /v1/tracks/{track_id}/audio` route. Given an authenticated user who owns a READY track, return the MP3 via `FileResponse` with correct headers. AC#7 (Cache-Control) is a one-line header addition verified by an extra assertion in the same test — not worth a separate slice.

Includes `audio_stream_started` log event per spec Telemetry section.

- Acceptance criteria: AC#1, AC#7
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` (add `stream_audio` handler)
  - `services/api/tests/e2e/test_audio_stream.py` (new)
- Failing test first: `test_stream_audio_returns_200_with_correct_headers_for_ready_track`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py -v`

### Slice 2: Audio streaming — Range request support (AC#2)

Test-only slice: verify that FastAPI's `FileResponse` serves `206 Partial Content` with correct `Content-Range` header for Range requests. No production code expected unless FileResponse doesn't handle Range natively, in which case add a custom streaming response.

- Acceptance criteria: AC#2
- Files:
  - `services/api/tests/e2e/test_audio_stream.py` (add range test)
- Failing test first: `test_stream_audio_range_request_returns_206_partial_content`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py::test_stream_audio_range_request_returns_206_partial_content -v`

### Slice 3: Audio streaming — auth guard (AC#6)

Return 401 for unauthenticated requests. The existing auth middleware (`current_user_id`) should handle this already — this slice adds a characterization test in the audio stream test file to confirm.

- Acceptance criteria: AC#6
- Files:
  - `services/api/tests/e2e/test_audio_stream.py` (add unauthenticated test)
- Failing test first: `test_stream_audio_returns_401_without_auth`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py::test_stream_audio_returns_401_without_auth -v`

### Slice 4: Audio streaming — ownership + readiness guards (AC#3, AC#4)

Return 404 for tracks the user doesn't own and tracks not in READY state. Two closely related guards in the same handler code path.

- Acceptance criteria: AC#3, AC#4
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` (guards in `stream_audio` handler)
  - `services/api/tests/e2e/test_audio_stream.py` (add 2 tests)
- Failing test first: `test_stream_audio_returns_404_for_track_not_owned_by_user`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py -k "not_owned or not_ready" -v`

### Slice 5: Audio streaming — missing file on disk (AC#5)

Return 404 and emit `audio_file_missing` warning log when audio_ref points to a nonexistent file.

- Acceptance criteria: AC#5
- Files:
  - `services/api/src/altune/adapters/inbound/http/catalog/router.py` (file-exists check in handler)
  - `services/api/tests/e2e/test_audio_stream.py` (add missing-file test)
- Failing test first: `test_stream_audio_returns_404_and_logs_warning_for_missing_file`
- Verify: `cd services/api && uv run pytest tests/e2e/test_audio_stream.py::test_stream_audio_returns_404_and_logs_warning_for_missing_file -v`

### Slice 6: Install expo-av + playback types (AC#8 — part 1)

Install `expo-av`. Define `PlaybackState` discriminated union type and `PlaybackContextValue` interface.

- Acceptance criteria: AC#8 (dependency + types foundation)
- Files:
  - `apps/mobile/package.json` (add expo-av)
  - `apps/mobile/src/features/playback/types.ts` (new)
- Failing test first: N/A — dependency install + type definitions (no runtime behavior)
- Verify: `cd apps/mobile && npx expo start` (confirm app loads without crash)

### Slice 7: PlaybackProvider + usePlayback hook (AC#8 — part 2)

Create the `PlaybackProvider` React context that configures expo-av audio mode on mount. Create `usePlayback` consumer hook. Wire into root `_layout.tsx`.

- Acceptance criteria: AC#8
- Files:
  - `apps/mobile/src/features/playback/hooks/PlaybackProvider.tsx` (new)
  - `apps/mobile/src/features/playback/hooks/usePlayback.ts` (new)
  - `apps/mobile/src/features/playback/__tests__/PlaybackProvider.test.tsx` (new)
  - `apps/mobile/src/app/_layout.tsx` (wrap tree with PlaybackProvider)
- Failing test first: `test_playback_provider_configures_audio_mode_on_mount`
- Verify: `cd apps/mobile && npx jest --testPathPattern=PlaybackProvider`

### Slice 8: Play a track from URL (AC#9, AC#10)

Wire `play(trackId)` to load `{API_BASE}/v1/tracks/{trackId}/audio` with Bearer token into `Audio.Sound` and begin playback. AC#10 (navigation persistence) is structural — guaranteed by the provider being in the root layout.

- Acceptance criteria: AC#9, AC#10
- Files:
  - `apps/mobile/src/features/playback/hooks/PlaybackProvider.tsx` (implement play/pause/resume/seekTo)
  - `apps/mobile/src/features/playback/api/audio.ts` (new — builds audio URL + headers)
  - `apps/mobile/src/features/playback/__tests__/PlaybackProvider.test.tsx` (add play test)
- Failing test first: `test_play_loads_audio_url_with_bearer_token`
- Verify: `cd apps/mobile && npx jest --testPathPattern=PlaybackProvider` + manual (tap play, hear audio)

### Slice 9: Background audio (AC#11)

Verify playback continues when app is backgrounded or screen is locked. Add `android.foregroundService` config to `app.json` if needed. Add a config assertion test.

- Acceptance criteria: AC#11
- Files:
  - `apps/mobile/app.json` (add `android.foregroundService` if needed)
  - `apps/mobile/src/features/playback/__tests__/PlaybackProvider.test.tsx` (add config assertion)
- Failing test first: `test_audio_mode_enables_stays_active_in_background`
- Verify: `cd apps/mobile && npx jest --testPathPattern=PlaybackProvider` + manual (play track, lock phone, confirm audio continues)

### Slice 10: MiniPlayer — render states (AC#12, AC#13)

Create MiniPlayer component. Renders artwork, title, artist, progress bar when a track is loaded. Not rendered when no track is loaded.

- Acceptance criteria: AC#12, AC#13
- Files:
  - `apps/mobile/src/features/playback/ui/MiniPlayer.tsx` (new)
  - `apps/mobile/src/app/(tabs)/_layout.tsx` (render MiniPlayer above TabBar)
  - `apps/mobile/src/features/playback/__tests__/MiniPlayer.test.tsx` (new)
- Failing test first: `test_mini_player_renders_track_info_when_loaded`
- Verify: `cd apps/mobile && npx jest --testPathPattern=MiniPlayer`

### Slice 11: MiniPlayer — play/pause toggle (AC#14)

Tapping the play/pause button on the MiniPlayer toggles playback state.

- Acceptance criteria: AC#14
- Files:
  - `apps/mobile/src/features/playback/ui/MiniPlayer.tsx` (wire play/pause)
  - `apps/mobile/src/features/playback/__tests__/MiniPlayer.test.tsx` (add toggle test)
- Failing test first: `test_mini_player_play_pause_toggles_playback`
- Verify: `cd apps/mobile && npx jest --testPathPattern=MiniPlayer`

### Slice 12: FullPlayer — rendering (AC#16)

Create FullPlayer component with large artwork, title, artist, scrubber, elapsed/remaining time, play/pause button.

- Acceptance criteria: AC#16
- Files:
  - `apps/mobile/src/features/playback/ui/FullPlayer.tsx` (new)
  - `apps/mobile/src/features/playback/ui/Scrubber.tsx` (new)
  - `apps/mobile/src/features/playback/__tests__/FullPlayer.test.tsx` (new)
- Failing test first: `test_full_player_renders_artwork_title_artist_scrubber`
- Verify: `cd apps/mobile && npx jest --testPathPattern=FullPlayer`

### Slice 13: FullPlayer — navigation (AC#15, AC#18)

Tapping MiniPlayer opens FullPlayer as a modal. Tapping close/minimize dismisses the modal and returns to MiniPlayer.

- Acceptance criteria: AC#15, AC#18
- Files:
  - `apps/mobile/src/app/player.tsx` (new — modal route)
  - `apps/mobile/src/features/playback/ui/MiniPlayer.tsx` (wire tap to navigate)
  - `apps/mobile/src/features/playback/ui/FullPlayer.tsx` (wire close button)
  - `apps/mobile/src/features/playback/__tests__/MiniPlayer.test.tsx` (add navigation test)
- Failing test first: `test_mini_player_tap_opens_full_player_modal`
- Verify: `cd apps/mobile && npx jest --testPathPattern=MiniPlayer` + manual (tap mini-player, see full player, dismiss)

### Slice 14: Seeking via scrubber (AC#17)

Dragging the scrubber calls `seekTo(ms)` on the PlaybackProvider. Elapsed/remaining time update during drag.

- Acceptance criteria: AC#17
- Files:
  - `apps/mobile/src/features/playback/ui/Scrubber.tsx` (wire drag to seekTo)
  - `apps/mobile/src/features/playback/__tests__/Scrubber.test.tsx` (new)
- Failing test first: `test_scrubber_drag_calls_seek_to_with_correct_position`
- Verify: `cd apps/mobile && npx jest --testPathPattern=Scrubber`

### Slice 15: Disabled play for non-ready tracks (AC#19)

Tracks without `acquisition_status == "ready"` show a disabled play button with a status indicator. Guard lives in the detail screen where the play button is rendered.

- Acceptance criteria: AC#19
- Files:
  - `apps/mobile/src/features/playback/helpers/canPlay.ts` (new — pure helper)
  - `apps/mobile/src/features/detail/ui/DetailScreen.tsx` (integrate play button with canPlay guard)
  - `apps/mobile/src/features/playback/__tests__/canPlay.test.ts` (new)
- Failing test first: `test_can_play_returns_false_for_pending_and_failed_tracks`
- Verify: `cd apps/mobile && npx jest --testPathPattern=canPlay`

### Slice 16: Playback failure error state (AC#20)

When playback fails (network error, 404), the mini-player shows an error state and playback stops cleanly.

- Acceptance criteria: AC#20
- Files:
  - `apps/mobile/src/features/playback/hooks/PlaybackProvider.tsx` (error handler on Audio.Sound status callback)
  - `apps/mobile/src/features/playback/ui/MiniPlayer.tsx` (error state rendering)
  - `apps/mobile/src/features/playback/__tests__/PlaybackProvider.test.tsx` (add error test)
- Failing test first: `test_playback_error_sets_error_state_and_stops_cleanly`
- Verify: `cd apps/mobile && npx jest --testPathPattern=PlaybackProvider`

### Slice 17: Accessibility labels (AC#21)

All playback controls get `accessibilityLabel` / `accessibilityRole`. MiniPlayer announces current track when focused.

- Acceptance criteria: AC#21
- Files:
  - `apps/mobile/src/features/playback/ui/MiniPlayer.tsx` (add labels)
  - `apps/mobile/src/features/playback/ui/FullPlayer.tsx` (add labels)
  - `apps/mobile/src/features/playback/ui/Scrubber.tsx` (add labels)
  - `apps/mobile/src/features/playback/__tests__/MiniPlayer.test.tsx` (add a11y assertions)
  - `apps/mobile/src/features/playback/__tests__/FullPlayer.test.tsx` (add a11y assertions)
  - `apps/mobile/src/features/playback/__tests__/Scrubber.test.tsx` (add a11y assertions)
- Failing test first: `test_mini_player_has_accessibility_labels`
- Verify: `cd apps/mobile && npx jest --testPathPattern="MiniPlayer|FullPlayer|Scrubber"`

## Post-implementation

- Run `/update-nested-claude-md apps/mobile/src/features/playback` — new feature dir needs a CLAUDE.md.
- Run `/verify-end-to-end` before code review.

## Risks

- **expo-av background audio on Android** — may require `android.foregroundService` config in `app.json`. Mitigated by testing on a real Android device in slice 9.
- **Bearer token expiry during seek** — unlikely for a ~10MB file. Fix by refreshing token before `seekTo` if it surfaces. Not blocking MVP.
- **FileResponse Range on Windows dev** — FastAPI delegates to starlette; Range support should work cross-platform. Verified by slice 2's characterization test.
- **Anti-pattern: fat provider** [vault: wiki/concepts/Vertical Slice Architecture.md] — PlaybackProvider kept focused on audio state; UI logic in MiniPlayer/FullPlayer.
- **Anti-pattern: adapter doing business logic** [vault: wiki/concepts/Hexagonal Architecture.md] — streaming endpoint must stay a thin shell (auth check + file serve). No domain logic, transcoding, or content negotiation in the handler.

## ADR candidates

- **ADR: expo-av adoption** — managed Expo dependency, Expo Go compatible. Document in commit body unless reviewer wants a full ADR.
- **ADR-0008 update** — only if background audio requires `app.json` foreground service config (determined in slice 9).
