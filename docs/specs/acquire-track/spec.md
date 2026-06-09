# Acquire Track

> Spec for `acquire-track` — version 1, drafted 2026-06-08.
> Authors: solo + Claude.
> Status: Draft.

## Problem

Users save tracks from discovery to their library, but every track sits at `acquisition_status = pending` forever. The library is a list of metadata with no audio behind it. There is no pipeline to turn a saved track into a playable file. Without audio acquisition, the app discovers music but can never play it — defeating its core purpose.

A predecessor project (music-manager) had an acquisition pipeline, but it frequently downloaded the wrong audio for a given track. Inaccurate matches undermine the entire streaming experience — downloading the wrong song is worse than downloading nothing.

## User value

When a user saves a track from discovery, the backend automatically acquires the correct audio in the background. The track transitions from `pending` to `ready`, and the user sees this reflected in their library. The audio file is stored on the server, ready for a future streaming spec to serve it.

## Scope tier / MVP cut

- **Minimal (ship this):** Single-track acquisition triggered automatically on save. yt-dlp downloads audio. Match verification ensures the downloaded audio corresponds to the intended track. Audio stored on server filesystem. Track updated to `ready` with `audio_ref` set. Acquisition status visible in library. Failed acquisitions surface an error state.
- **Deferred to post-launch:** Real-time progress updates (SSE/WebSocket), retry UI on mobile, concurrent download queue management, multiple audio format/quality options, content-addressed storage, cross-user audio dedup, transcoding pipeline, download speed throttling, admin dashboard for failed acquisitions.
- **Justified exceptions:** Match verification is pulled into scope despite being "quality infrastructure" — needed now because the predecessor project proved that blind downloads produce wrong audio, and shipping inaccurate playback is worse than shipping no playback.

The acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

1. **AC#1 — Auto-trigger on save.** Given a user saves a new track via `POST /v1/tracks` (created=true), when the save use case returns `created=true`, then the inbound HTTP adapter triggers the `AcquireTrackAudio` use case in a background task. The HTTP response (201) returns immediately; the acquisition runs outside the request lifecycle.
2. **AC#2 — Tiered search waterfall.** Given a track with an ISRC, then Tier 1 searches YouTube Music by ISRC (`ytmsearch:{isrc}`). Given a track without an ISRC (or Tier 1 fails), then Tier 2 searches YouTube Music with `{title} {artist}`. If Tier 2 fails and album is known, Tier 3 searches YouTube Music with `{title} {artist} {album}`. If all YTM tiers fail, Tier 4 searches regular YouTube with `{title} {artist}`. Top 5 candidates per tier. Stop at first tier that produces a passing candidate.
3. **AC#3 — Gate-based match verification.** Given candidate audio results from yt-dlp info extraction (no download yet), when selecting which result to download, then each candidate is checked against three hard pass/fail gates: (a) title gate — normalized Jaro-Winkler between track title and candidate title >= 0.85, (b) artist gate — normalized JW between track artist and candidate uploader/artist >= 0.70, (c) duration gate — absolute difference <= 15 seconds (skipped if track has no `duration_seconds`). Normalization reuses the existing `normalize_for_match` pipeline (NFKC, casefold, strip brackets/feat, collapse whitespace). From candidates passing all gates, the one with the highest title JW is selected. If zero candidates pass, acquisition fails with `no_match_found`. ISRC matches (Tier 1) bypass gates — ISRC is a recording-level identifier; duration is checked as a sanity warning only (logged, not rejected). Gate values are fixed implementation constants, not tunable config.
4. **AC#4 — Audio download.** Given an accepted candidate, when downloading, then yt-dlp extracts the best available audio and converts to MP3 at 320kbps via FFmpeg post-processing.
4a. **AC#4a — ID3 tagging.** Given a downloaded MP3, when post-processing, then ID3v2.4 tags are written: title, artist, album, year, track_number, album_artist, genre. Album art from the track's `artwork_url` is fetched and embedded. The file is self-describing and plays correctly in any player.
5. **AC#5 — Post-download duration check.** Given a downloaded audio file and a track with known `duration_seconds`, when the download completes, then the actual file duration is compared against the expected duration. A mismatch beyond +/-15 seconds logs a warning with `duration_mismatch` tag but does not reject the file (live recordings, remasters, fade-outs may differ). If `duration_seconds` is null on the track, this check is skipped.
6. **AC#6 — File storage.** Given a successfully downloaded audio file, when storing, then the file is moved to `{MUSIC_DIR}/{user_id}/{artist}/{album}/{title}.mp3` (sanitized filenames), and the relative path (everything after `MUSIC_DIR/`) is recorded as the track's `audio_ref`. Path collision between different tracks cannot occur: the `UNIQUE(user_id, dedup_key)` constraint ensures that two tracks with the same (title, artist, album) dedup to a single row, so the path is unique per track. If the file already exists on disk (re-acquisition after deletion), it is overwritten.
7. **AC#7 — Track transitions to ready.** Given a successful acquisition, when the file is stored and `audio_ref` is set, then `acquisition_status` transitions from `pending` to `ready`. The bidirectional invariant (`audio_ref` non-null <-> `ready`) is enforced by the domain.
8. **AC#8 — Failed acquisition surfaces error.** Given an acquisition that fails (no match found, download error, network failure), when the failure occurs, then the track's `acquisition_status` transitions to `failed` and a `failure_reason: str | None` field on the Track aggregate is set with a human-readable reason (e.g., `"no_match_found"`, `"download_error: 403 Forbidden"`, `"network_timeout"`). The track remains in the library — it is not deleted.
9. **AC#9 — Failed status visible on wire.** Given a track with `acquisition_status = failed`, when the user fetches their library via `GET /v1/tracks`, then the `TrackResponse` includes `acquisition_status: "failed"` and `failure_reason: string | null`. The mobile client renders failed tracks with an error indicator (red/warning badge or text). No new screens — the existing library list handles the new state inline.
10. **AC#10 — Idempotent acquisition.** Acquisition only triggers when the `AddTrackToLibrary` use case returns `created=true`. The `UNIQUE(user_id, dedup_key)` database constraint ensures a second save for the same (title, artist, album) returns `created=false`, so no duplicate acquisition fires. No in-process lock or `ACQUIRING` state is needed for v1 — the dedup constraint is the idempotency mechanism.
11. **AC#11 — Acquisition survives request lifecycle.** Given an acquisition is in progress, when the HTTP request that triggered it has already returned 201, then the acquisition continues to completion. The user does not wait for the download.
12. **AC#12 — Temporary file cleanup.** Given a failed or interrupted acquisition, when the process exits the acquisition path, then any temporary files are cleaned up. No orphaned temp files accumulate.
13. **AC#13 — Metadata enrichment on save.** Given a user saves a track from discovery, when the `CreateTrackRequest` is constructed on mobile, then `isrc`, `year`, `genre`, and `album_artist` from the discovery result's `extras` map are included in the request. These fields flow through to the Track aggregate, giving the acquisition pipeline maximum signals for matching (especially ISRC for Tier 1).

### AcquisitionStatus state machine

```
PENDING ──(acquisition succeeds)──> READY
PENDING ──(acquisition fails)────> FAILED
```

**v1 valid transitions:** `PENDING → READY`, `PENDING → FAILED`. No other transitions exist in this spec.

**Deferred transitions (future specs):** `FAILED → PENDING` (retry), `READY → PENDING` (re-acquire after file loss).

**No `ACQUIRING` intermediate state in v1.** Tracks remain `PENDING` while acquisition is in progress. The user sees `pending` until it resolves to `ready` or `failed`. An `ACQUIRING` state adds complexity (server-restart recovery, stuck-state detection) with minimal user value for a solo pre-launch app. Deferred to post-launch queue-management spec.

**Invariant interactions:** `FAILED` has `audio_ref = None` — compatible with the existing bidirectional invariant (which only constrains `READY ↔ audio_ref`). The new `failure_reason` field is `None` for `PENDING` and `READY` tracks; non-null only for `FAILED`.

**Server restart:** In-progress acquisitions (background tasks) are lost on restart. Tracks remain `PENDING` indefinitely. Acceptable for pre-launch; a startup reconciliation job (re-trigger all `PENDING` tracks) is a post-launch enhancement.

## Out of scope

- **Streaming / playback.** This spec acquires and stores audio. Serving it to the mobile player is `stream-playback`.
- **Real-time progress.** No SSE, WebSocket, or polling endpoint for download progress. The user sees `pending` -> `ready` (or `failed`) on next library fetch.
- **Retry from mobile.** No "retry download" button or endpoint. Failed tracks stay `failed` until a future spec adds retry.
- **Queue management / concurrency limits.** If 20 tracks are saved rapidly, 20 acquisitions start. No queue, no rate limiting, no priority ordering. Acceptable for solo pre-launch; queue management is a post-launch spec.
- **Multiple formats / quality selection.** MP3 only. No user-configurable bitrate or format choice.
- **Content-addressed storage / cross-user dedup.** Each user gets their own copy. Dedup is a post-launch optimization.
- **Audio fingerprinting (Chromaprint/AcoustID).** Match verification uses metadata comparison only. Acoustic fingerprinting is a post-launch accuracy enhancement.
- **Album-level batch acquisition.** One track at a time. "Download entire album" is a future spec.
- **Provider fallback chains.** v1 uses a single yt-dlp search strategy. Multi-provider fallback (try YouTube Music, then YouTube, then SoundCloud) is a post-launch enhancement.
- **Acquisition from direct URL.** v1 always searches by metadata. "Download from this specific URL" is a future spec.
- **Mobile UI changes beyond status display.** No new screens, no progress bars, no download animations. The existing library list already shows `acquisition_status` — `failed` is a new value it must handle.

## Design considerations

- [vault: wiki/concepts/Message Queue.md] — Background job processing pattern. Full message queue (RabbitMQ/Kafka) is deferred for pre-launch; minimal tier uses in-process async task execution. The vault note's "When to use" section confirms queues are for "distributing work across multiple competing consumers" — overkill for a single-server solo app.
- [vault: wiki/topics/Messaging Patterns Overview.md] — Delivery semantics. At-least-once + idempotent consumer is the pragmatic choice. The Track's dedup key + acquisition status check provide natural idempotency.

**High-level approach:**
- This is a **write path** in the `catalog` bounded context (it mutates the Track aggregate).
- It **does** require a new domain state (`AcquisitionStatus.FAILED`), a new Track field (`failure_reason`), and new domain events (`TrackAcquisitionCompleted`, `TrackAcquisitionFailed`).
- It **does** require new ports: `AudioSearcher` (search + download via yt-dlp) and `AudioStore` (filesystem storage). Both in the application layer, implemented by outbound adapters.
- It **does** introduce new external dependencies: yt-dlp + FFmpeg (audio), mutagen or eyed3 (ID3 tagging).
- The Track aggregate is immutable (frozen dataclass). State transitions create new instances. The `TrackRepository` port needs `get_by_id` and `update` methods added.

**Pipeline architecture (from brainstorm):**
- 6-step pipeline with explicit Step objects (execute/rollback) and a Pipeline runner: SearchStep → SelectStep → DownloadStep → TagStep → StoreStep → UpdateTrackStep.
- Pipeline runner orchestrates rollback on failure, sets Track to FAILED with reason, emits failure event.
- Steps share an `AcquisitionContext` mutable dataclass. See `design.md` for full step definitions.

**Match verification (the core accuracy concern, from brainstorm):**
- Gate-based, not score-based. Three hard pass/fail gates (title JW >= 0.85, artist JW >= 0.70, duration within ±15s). No weighted composite, no tunable threshold.
- 4-tier search waterfall: ISRC on YTM → title+artist on YTM → title+artist+album on YTM → title+artist on YouTube.
- ISRC tier bypasses gates (recording-level identifier).
- This design directly addresses the predecessor's accuracy failure: it scored nothing and downloaded the first result. This design scores every candidate against hard gates and rejects below the bar.

### Glossary additions (to commit with spec)

- **AcquisitionStatus.FAILED** — acquisition attempted and failed; `audio_ref` is null, `failure_reason` is set. Wire-serialized as `"failed"`.
- **failure_reason** — human-readable string on a FAILED Track explaining why acquisition failed (e.g., `"no_match_found"`, `"download_error"`). Null for PENDING and READY tracks. Field on the Track aggregate, persisted in the `tracks` table, returned on the wire.
- **TrackAcquisitionCompleted** / **TrackAcquisitionFailed** — catalog domain events (past-tense, immutable). Emitted to logs by the `AcquireTrackAudio` use case.
- **AudioSearcher** — application port for searching and downloading audio by metadata. Implemented by a yt-dlp adapter.
- **AudioStore** — application port for persisting audio files to storage. Implemented by a filesystem adapter.

## Dependencies

- **Bounded contexts**: `catalog` (Track aggregate, TrackRepository port — port needs `get_by_id` and `update` methods added)
- **Other features**: `view-result-detail` (creates PENDING tracks — already shipped)
- **External services**: yt-dlp (CLI tool, pip-installable), FFmpeg (system dependency for audio conversion)
- **Library/framework additions**: yt-dlp Python package; no new framework additions
- **Configuration**: `MUSIC_DIR` env var (base path for audio storage, e.g., `/mnt/oci-music`) must be added to `platform/config.py` Settings

## Risks / open questions

- **Risk: yt-dlp breakage.** YouTube periodically changes its internals, breaking yt-dlp extractors. Mitigation: yt-dlp is actively maintained and self-updates; pin to a known-good version and update periodically. The `failed` status means breakage is surfaced, not silent.
- **Risk: Match accuracy still insufficient.** Metadata comparison may not catch all mismatches (e.g., karaoke versions with identical titles and durations). Mitigation: this is the known limitation of the minimal tier; audio fingerprinting is the deferred enhancement. Log match confidence scores to measure accuracy in production.
- **Risk: Long-running downloads block the server.** A single yt-dlp download can take 10-60 seconds. Mitigation: acquisition runs in a background task, not in the request lifecycle. The HTTP response returns immediately.
- **Risk: Disk space exhaustion.** No storage quota or cleanup mechanism. Mitigation: acceptable for solo pre-launch with a known small user base. Monitoring/alerting is a post-launch concern.
- **Resolved: MP3 bitrate is 320kbps.** Matches legacy quality. No configurability in v1.
- **Resolved: File path is `{user_id}/{artist}/{album}/{title}.mp3`.** Human-readable hierarchy with filename sanitization. Matches legacy structure but uses UUID user_id instead of email.

## Telemetry

- **Log events**: `TrackAcquisitionStarted(track_id, user_id, search_strategy)`, `TrackAcquisitionCompleted(track_id, user_id, duration_ms, match_confidence, file_size_bytes, candidate_title, candidate_channel, expected_title, expected_artist)`, `TrackAcquisitionFailed(track_id, user_id, reason, search_query, candidates_evaluated)`. The completed event carries enough detail for manual accuracy spot-checks.
- **Metrics (log-derived for v1)**: acquisition success rate, average acquisition duration, match confidence distribution, failure reasons breakdown.
- **Alerts**: none for v1 (log-only). Post-launch: alert on sustained failure rate > threshold.

## Related

- [vault: wiki/concepts/Message Queue.md] — background processing patterns
- [vault: wiki/topics/Messaging Patterns Overview.md] — delivery semantics and reliability
- Related ADRs: none yet (yt-dlp is a tool choice, not an architectural commitment)
- Predecessor: `view-result-detail` spec (creates PENDING tracks)
- Successor: `stream-playback` spec (serves audio from `audio_ref`)
- Legacy reference: `C:\Users\Alessandro\music-manager` (7-step download pipeline — reference for what NOT to copy blindly re: match accuracy)
