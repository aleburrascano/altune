# Import legacy library

> Spec for `import-legacy-library` — version 1, drafted 2026-06-04.
> Authors: solo + Claude.
> Status: Draft.

## Problem

The user's music library (1,560+ tracks) lives in a legacy Supabase database with a schema designed
for the old music-manager app. Altune's `tracks` table exists but is missing columns the legacy data
carries (year, genre, track_number, album_artist, ISRC, audio file references). Without extending the
schema and importing the data, the library is empty and the app has no content.

## User value

After this ships, the user's entire existing music collection appears in Altune's Library tab with
full metadata (genre, year, track number, album artist, ISRC) and audio references pointing to their
existing OCI-stored MP3 files — ready for the streaming spec to wire up playback.

## Scope tier / MVP cut

- **Minimal (ship this):** Extend Track with 6 columns, add `AcquisitionStatus.READY`, run a one-shot
  import of the user's ~1,560 songs with column mapping and idempotency.
- **Deferred to post-launch:** Multi-user import (other users self-service), streaming endpoint,
  content-addressed storage dedup, genre/year browse indices, data validation UI.
- **Justified exceptions:** none.

## Acceptance criteria

1. **AC#1** — Given the `tracks` table, when the Alembic migration runs, then 6 new nullable columns
   exist: `year` (integer), `genre` (text), `track_number` (integer), `album_artist` (text),
   `isrc` (text), `audio_ref` (text). No existing data is broken.

2. **AC#2** — Given the `Track` domain aggregate, when constructed with the new optional fields, then
   `year` (when present) is positive, `track_number` (when present) is positive, and all other new
   fields are optional with no additional invariants.

3. **AC#3** — Given a Track with `audio_ref` set (non-null), then `acquisition_status` must be `ready`.
   Given a Track with `audio_ref = None`, then `acquisition_status` must be `pending`.

4. **AC#4** — Given `AcquisitionStatus`, when the enum is defined, then it has exactly two members:
   `PENDING` and `READY`. Wire-serialized lowercase.

5. **AC#5** — Given the import script, when run against the old Supabase `songs` table filtered to
   `user_id = c5d0d898-1b52-43a0-80b5-47a25f03ffb6`, then it fetches all ~1,560 rows (paginated).

6. **AC#6** — Given a legacy `songs` row, when the import script maps it, then the column mapping is:
   - `title` → `title` (as-is)
   - `artist` → `artist` (as-is)
   - `album` → `album` (null preserved)
   - `round(duration)` → `duration_seconds` (float→int)
   - `album_art` → `artwork_url` (as-is)
   - `album_artist` → `album_artist` (as-is)
   - `year` → `year` (as-is)
   - `genre` → `genre` (as-is)
   - `track_number` → `track_number` (as-is)
   - `isrc` → `isrc` (as-is)
   - `added_date` → `added_at` (preserves original library ordering)
   - `file_path` → `audio_ref` (strip `/mnt/oci-music/<email>/` prefix, prepend `<new_user_id>/`)
   - `acquisition_status` = `ready` (files exist on OCI)
   - `dedup_key` = computed via existing `dedup_key(title, artist, album)` function
   - `user_id` = user's new Altune Supabase auth UUID

7. **AC#7** — Given the import script, when it inserts rows, then it uses
   `INSERT ... ON CONFLICT (user_id, dedup_key) DO NOTHING` so running twice is safe —
   duplicates are silently skipped.

8. **AC#8** — Given the import completes, when the script reports results, then it prints counts:
   inserted / skipped (dedup) / errored.

9. **AC#9** — Given the existing `GET /v1/tracks` endpoint, when called after import, then the
   response includes the new fields (`year`, `genre`, `track_number`, `album_artist`, `isrc`,
   `audio_ref`, `acquisition_status: ready`) for imported tracks.

10. **AC#10** — Given the mobile Library screen, when displaying imported tracks, then `LibraryRow`
    renders them with `acquisition_status = ready` (the existing display logic already handles
    the status field — imported tracks no longer show the `pending` marker).

## Out of scope

- Audio streaming endpoint (wiring `audio_ref` to actual file serving) — `stream-playback` spec.
- Content-addressed storage / cross-user audio dedup — future evolution.
- Import of other users' songs — self-service import spec.
- Import of `download_queue`, `metadata_issues`, `user_profiles`, `schema_version` tables — not needed.
- Genre/year browse, filtering, or search by metadata — future browse specs.
- Mobile UI for displaying the new metadata fields (year, genre, track_number, album_artist, ISRC)
  on the detail screen — existing detail screen already shows extras; new fields show via the
  existing `GET /v1/tracks` response shape.
- Migration from per-user paths to content-addressed storage — deferred.

## Design considerations

- [vault: wiki/concepts/Aggregate.md] — Track remains the aggregate root. Adding fields is extending
  the root, not violating its boundary. The `audio_ref ↔ acquisition_status` invariant is enforced
  in the constructor.
- [vault: wiki/concepts/Repository Pattern.md] — `TrackRepository.add()` already uses
  `ON CONFLICT DO NOTHING`; the import script reuses this idempotency pattern via bulk insert.
- [vault: wiki/concepts/Value Object.md] — `AcquisitionStatus` gains `READY` as a second member.
  Still an enum value object — immutable, identity-less, compared by value.

High-level approach:
- This is a **write** path in the `catalog` bounded context.
- It **extends** the existing `Track` aggregate with 6 optional fields and adds `READY` to
  `AcquisitionStatus`. No new aggregate or port.
- It **does not** introduce a new external dependency (old Supabase is read-only source, accessed
  only by the import script via HTTP, not a runtime dependency).
- Import script lives under `services/api/scripts/` — a one-shot CLI tool, not a production endpoint.

## Dependencies

- **Bounded contexts**: `catalog` (Track aggregate + `tracks` table — extended by this spec).
- **Other features**: `view-library` (displays tracks after import), `view-result-detail` (existing
  Track schema being extended).
- **External services**: Old Supabase REST API (read-only, one-time).
- **Library/framework additions**: none (urllib or httpx for the import script; SQLAlchemy already present).

## Risks / open questions

- **Risk**: Old Supabase Deezer CDN `artwork_url` links may expire — mitigation: acceptable;
  the URLs are long-lived CDN paths, not signed. Re-enrichment spec can refresh if needed.
- **Risk**: `file_path` → `audio_ref` transform may not cover all path formats — mitigation:
  sample all distinct path prefixes before transform, log any that don't match the expected pattern.
- **Open question**: User's new Altune Supabase auth UUID — to be resolved at import time by
  querying the auth session or accepting it as a CLI argument.
- **Risk**: Songs in old DB with empty title or artist would violate Track invariants — mitigation:
  skip and log; import script reports these in the error count.

## Telemetry

- **Log events**: Import script logs start/end with row counts. No runtime domain events for import
  (these are bulk-loaded, not user-initiated saves; `TrackAddedToLibrary` is not emitted).
- **Metrics**: none (one-shot script, not a recurring operation).
- **Alerts**: none.

## Related

- Predecessor: `docs/specs/view-result-detail/spec.md` (introduced Track.artwork_url,
  acquisition_status, dedup_key — this spec extends all three).
- Predecessor: `docs/specs/view-library/spec.md` (the read path that displays imported tracks).
- Successor: `stream-playback` (will use `audio_ref` to serve audio).
