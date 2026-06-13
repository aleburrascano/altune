# Data Resilience

> Spec for `resilience-v1` — version 1, drafted 2026-06-13.
> Authors: solo + Claude.
> Status: Clarify-gated.

## Problem

The system does not enforce consistency between its data stores. Audio files can be deleted from disk while the database still marks the track as `ready`, leading to playback failures that lock up the player. Duplicate track rows from past bugs persist with no cleanup mechanism. Tracks with broken metadata ("no match found") linger in the library. When any part of the data goes stale, nothing notices — the user encounters the inconsistency first, and the system does nothing to recover.

This will get worse as features grow. Metadata editing, playlist management, and any future service that touches track data will inherit the same fragility unless the system enforces consistency as a cross-cutting concern.

## User value

When this ships:
- Playing a track that has no audio file shows a clear error and marks the track as failed — the user is never stuck on a disabled play button.
- Duplicate tracks from past bugs are cleaned up — each song appears exactly once.
- The library never shows ghost tracks with broken metadata.
- The system self-corrects: when it detects a data inconsistency (missing file, orphaned row, broken reference), it cascades the fix to the database and notifies the UI.
- Any future feature or service that touches track data adopts the same consistency contract.

## Scope tier / MVP cut

- **Minimal (ship this):**
  1. Reactive cascade on stream 404 — refactor existing ad-hoc logic from the adapter into a `ReconcileTrackStatus` use case, add test coverage.
  2. Storage health-check command (CLI sweep: DB tracks marked READY vs. files on disk).
  3. Retroactive dedup cleanup (one-time SQL migration).
  4. UI handling of failed/missing tracks (visual treatment, delete option via new API client function).
  5. Consistency contract documented as a pattern for future features.
  6. Glossary update: add `failed` member to `AcquisitionStatus` in `docs/ubiquitous-language.md`.

- **Deferred to post-launch:**
  - Proactive scheduled sweeps (cron/worker) — run the health check manually for now.
  - Event bus / message queue for cross-service cascade — use in-process domain events for now.
  - Go microservices migration (Strangler Fig) — separate initiative.
  - Real-time push to mobile (WebSocket/SSE) — cache invalidation on pull-to-refresh is sufficient.
  - ML-based anomaly detection (Self-Healing Systems pattern).
  - Orphaned file deletion — health check reports orphaned files but does not delete them in v1.

- **Justified exceptions:** none — all deferred items are genuinely post-launch.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test.

### Reactive cascade

1. **AC#1 — Cascade logic lives in a use case, not the adapter.** The existing cascade code in `stream_audio` (router.py) is refactored into a `ReconcileTrackStatus` use case in `application/catalog/`. The stream handler calls the use case; the use case is independently testable with an in-memory repository. Given a track with `acquisition_status = ready` and `audio_ref` pointing to a missing file, when `ReconcileTrackStatus.execute(track_id, user_id)` is called, then the track is updated to `acquisition_status = failed`, `audio_ref = null`, `failure_reason = "Audio file missing from storage"`, and a `TrackMarkedFailed` domain event is emitted.

2. **AC#2 — Failed track is not playable.** Given a track with `acquisition_status = failed`, when the mobile UI renders the track in the library or detail screen, then the play button is not shown. (Verify: `canPlay` already returns false for `failed`; `LibraryRow` already renders `failure_reason`. This AC is a verification of existing behavior, plus ensuring the detail screen's `PlayIconButton` is gated the same way.)

3. **AC#3 — Playback recovers after failure.** Given the player's status is `error` (from a prior failed track A), when `play(trackB)` is called with a valid track, then the player's status transitions from `error` → `loading` → `playing` (or `paused` if auto-play is not configured). Specifically: `audioSource` is reset to `null` before the new source is set, `errorMessage` is cleared, and `shouldAutoPlay` is re-enabled. The play button for track B is not disabled.

4. **AC#4 — Library cache refreshes on playback failure.** Given the PlaybackProvider's 10-second loading timeout fires, then `library-home` and `library` React Query caches are invalidated. Note: the 10-second timeout is a client-side heuristic; it does NOT trigger the backend cascade (AC#1). The backend cascade is triggered only when the stream endpoint is actually called. Both paths result in the track being marked failed — the timeout catches cases where the HTTP request itself hangs or the device has no connectivity.

### Storage health check

5. **AC#5 — Health-check CLI detects orphaned DB rows.** Given N tracks in the database with `acquisition_status = ready`, and M of those tracks have `audio_ref` values pointing to files that do not exist on disk, when the health-check command is run, then it reports M orphaned tracks by ID and title.

6. **AC#6 — Health-check CLI can fix orphaned rows.** Given the health-check command detects M orphaned tracks, when run with `--fix`, then each orphaned track is updated via `ReconcileTrackStatus` (same use case as AC#1). Each change is logged individually.

7. **AC#7 — Health-check detects orphaned files.** Given files in the audio storage directory that are not referenced by any track's `audio_ref`, when the health-check command is run, then it reports those files as orphaned (report only — no deletion in v1).

### Retroactive dedup cleanup

8. **AC#8 — Dedup migration identifies duplicates.** Given tracks in the database where multiple rows share the same `dedup_key` column value for the same `user_id`, when the dedup migration is run in dry-run mode, then it reports the duplicate groups with their IDs, titles, and `acquisition_status`. The migration queries the `dedup_key` column directly in SQL (it is stored as a column with a UNIQUE constraint per user in the repository).

9. **AC#9 — Dedup migration keeps the best copy.** Given a duplicate group, when the migration runs, then it keeps the track with the highest-priority `acquisition_status` (`ready` > `pending` > `failed`), and among ties keeps the one with the earliest `added_at`. All other duplicates in the group are deleted. Playlist memberships pointing to deleted tracks are remapped to the kept track's ID, **except** when the kept track is already in the same playlist (skip the remap to preserve the no-duplicate-track_ids invariant; adjust positions to remain contiguous).

10. **AC#10 — Dedup migration is idempotent.** Given the dedup migration has already run, when it runs again, then it reports zero duplicates and makes no changes.

### UI failed-track handling

11. **AC#11 — Failed tracks show status in library.** Given a track with `acquisition_status = failed` in the library, when the library screen renders, then the track row shows the `failure_reason` text (already implemented in `LibraryRow`) and does not show a play affordance (verify `canPlay` gate). This AC is primarily a verification of existing behavior.

12. **AC#12 — Failed tracks can be deleted from library.** Given a failed track in the library, when the user long-presses the track, then a "Remove from Library" option is available, and selecting it calls `DELETE /v1/tracks/{track_id}` (backend endpoint already exists) via a new `deleteTrack(trackId)` function in the mobile API client (`shared/api-client/tracks.ts`), then invalidates the library cache and the track disappears from the UI.

### Acquisition progress

13. **AC#13 — Saving a track shows acquisition progress.** Given the user taps "Save to Library" on the detail screen, when the track is saved and acquisition begins, then the UI shows a loading/progress indicator on that track (in the detail screen's save button AND in the library row) that persists until `acquisition_status` transitions from `pending` to `ready` or `failed`. The library polls (or uses React Query's `refetchInterval`) to detect the transition without requiring manual pull-to-refresh.

14. **AC#14 — Pending tracks auto-refresh until resolved.** Given the library contains tracks with `acquisition_status = pending`, then the `library-home` query uses a `refetchInterval` (e.g., 5 seconds) until no pending tracks remain. This eliminates the need for manual pull-to-refresh while waiting for acquisition.

### Consistency contract (pattern)

15. **AC#15 — Consistency contract documented.** A document at `docs/patterns/data-consistency.md` describes the cascade pattern: when a data inconsistency is detected (at any layer), the detecting layer is responsible for (a) fixing the inconsistency in its own store, (b) logging the event with structured fields (entity_id, reason, user_id), and (c) signaling downstream consumers (cache invalidation, UI refresh). Future features reference this document. Any new service or bounded context that manages data with external dependencies (files, external APIs) must implement this pattern.

## Out of scope

- **Go microservices migration** — the consistency patterns built here are architecture-agnostic and will carry over to Go services when that migration begins. The Strangler Fig pattern [vault: wiki/concepts/Strangler Fig Pattern.md] is the planned approach but is a separate initiative.
- **Scheduled/automated sweeps** — the health-check is a CLI command run manually. Cron scheduling is post-launch.
- **Cross-service event bus** — cascade events are in-process domain events (method calls + log emissions). A message broker (Redis Pub/Sub, NATS) is post-launch when services are split.
- **User-facing data management** — bulk delete, merge duplicates in the UI, metadata editing. These are separate feature specs that will adopt the consistency contract.
- **Soft-delete / undo** — tracks are hard-deleted for simplicity. Soft-delete can be added later.
- **Orphaned file deletion** — health check reports orphaned files but does not delete them. Manual cleanup for now.

## Design considerations

Patterns surfaced from the vault:

- [vault: wiki/concepts/Self-Healing Systems.md] — the full three-stage ML framework (MAML + GNN + RL) is overkill for a solo app. But the core principle applies: **detect → fix → verify**. We implement the simplest version: rule-based detection (file exists? dedup key unique?), deterministic fix (mark failed, delete duplicate), verify (health-check re-run). This is Stage 0 of the self-healing roadmap.

- [vault: wiki/concepts/Reactive Architecture.md] — the "message-driven" property is the enabler. In our monolith, "messages" are domain events emitted by aggregates and consumed by the same process (log + cache invalidation). When services split, these become actual messages on a bus. Building the event semantics now means the Go migration inherits them.

- [vault: wiki/concepts/Strangler Fig Pattern.md] — relevant for future Go migration. The consistency patterns we build (health check, cascade, events) are the "new system components" that will eventually replace ad-hoc error handling. The façade is the API gateway (or Expo Router on mobile).

High-level approach:

- This is a **mixed read/write** path in the `catalog` bounded context. The stream handler lives in `adapters/inbound/http/catalog/` (not a separate `playback` context — no backend playback context exists).
- It requires a new **use case**: `ReconcileTrackStatus` in `application/catalog/` — called by the stream handler on file-not-found and by the CLI health check. This use case accepts a `TrackId` and a `reason`, loads the track, replaces it with `failed` status, saves, and emits a `TrackMarkedFailed` domain event.
- It requires a new **CLI adapter**: `adapters/inbound/cli/health_check.py` — invoked via `uv run python -m altune.adapters.inbound.cli.health_check` (or Typer if adopted via ADR).
- The mobile changes require a new **`deleteTrack` function** in `shared/api-client/tracks.ts` calling `DELETE /v1/tracks/{track_id}`. This is not UI-only.

## Dependencies

- **Bounded contexts**: `catalog` (Track aggregate, AcquisitionStatus, dedup_key, Playlist aggregate for remapping)
- **Other features**: `audio-playback-v1` (stream endpoint — already shipped), `import-legacy-library` (audio_ref field — already shipped), `playlists-v1` (playlist membership for dedup remapping)
- **External services**: none
- **Library/framework additions**: Typer for CLI commands (needs ADR — not in `pyproject.toml`). Fallback: plain `argparse` script if ADR is declined.

## Risks / open questions

- **Risk**: Dedup migration deletes the wrong copy (e.g., the one with `ready` status when another has `pending`). Mitigation: dry-run mode first (AC#8), priority logic documented (AC#9), idempotency check (AC#10).
- **Risk**: Health-check `--fix` runs against production and marks tracks as failed that have temporary file-system issues (NFS mount lag, disk full). Mitigation: log every change, require explicit `--fix` flag, report-only by default.
- **Risk**: Playlist remapping during dedup (AC#9) could create duplicate-track violations. Mitigation: skip remap when kept track already in playlist; adjust positions to stay contiguous (AC#9 specifies this).
- **Risk**: Dedup migration deletes tracks that other systems reference by `TrackId` (search clicks, analytics). Mitigation: currently only playlists hold FK references; search clicks reference `result_signature` not `TrackId`. Log all deletions for manual review.
- **Risk**: The 10-second PlaybackProvider timeout is a client heuristic. A slow-but-valid stream (large file, slow network) could be incorrectly marked as failed. Mitigation: 10s is generous for typical 5MB tracks; if false positives occur, increase timeout or switch to HTTP status-based detection post-launch.
- **Open question**: Should failed tracks auto-retry acquisition? — Defer to post-launch. For now, failed is terminal; the user can re-save the track to trigger a new acquisition.
- **Open question**: Should the health-check also verify that `pending` tracks haven't been stuck for too long? — Good idea, defer to v2. Define "too long" requires an acquisition SLA we don't have yet.

## Telemetry

- **Log events**:
  - `track_marked_failed` — emitted by `ReconcileTrackStatus` use case (track_id, reason, user_id)
  - `health_check_completed` — summary (total_checked, orphaned_db, orphaned_files, fixed)
  - `dedup_migration_completed` — summary (groups_found, tracks_deleted, playlists_remapped)
  - `track_deleted_by_user` — emitted when AC#12 delete is triggered (track_id, user_id)

- **Metrics** (post-launch):
  - `tracks_failed_total` — counter of tracks cascaded to failed status
  - `health_check_orphaned_count` — gauge from last health-check run
  - `dedup_duplicates_found` — gauge from last dedup run

- **Alerts** (post-launch):
  - `tracks_failed_rate > 5/hour` — something is systematically wrong with storage

## Related

- [vault: wiki/concepts/Self-Healing Systems.md] — advanced self-healing roadmap (Stage 0 here)
- [vault: wiki/concepts/Reactive Architecture.md] — message-driven consistency model
- [vault: wiki/concepts/Strangler Fig Pattern.md] — future Go migration pattern
- Related ADRs: none yet (Typer CLI may need one)
- Predecessor specs: `docs/specs/audio-playback-v1/spec.md`, `docs/specs/import-legacy-library/spec.md`
