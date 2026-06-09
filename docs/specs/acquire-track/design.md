# Acquire Track — Design

> Brainstormed 2026-06-08. Companion to `spec.md`.
> Decisions here override spec.md where they conflict; spec.md will be updated to match.

## Architecture

Acquisition lives in the `catalog` bounded context (it mutates the Track aggregate's lifecycle).

```
POST /v1/tracks → AddTrackToLibrary (existing)
                     │ created=true
                     ▼
              Background task → AcquireTrackAudio use case
                     │
              AcquisitionPipeline (6 steps)
                     │
         ┌───────────┼───────────┐
         ▼           ▼           ▼
   AudioSearcher  AudioStore  TrackRepository
     (port)        (port)      (existing, extended)
         │           │
         ▼           ▼
   YtDlpAdapter  FilesystemAdapter
```

### New ports (application layer)

- **AudioSearcher** — search for audio candidates by metadata, return candidate metadata without downloading. Also download a selected candidate to a temp path.
- **AudioStore** — persist an audio file to permanent storage, return a relative `audio_ref`.

### New adapters (outbound)

- **YtDlpAudioSearcher** — implements AudioSearcher via yt-dlp `extract_info` (search) and `download` (acquire).
- **FilesystemAudioStore** — implements AudioStore. Moves files to `{MUSIC_DIR}/{user_id}/{artist}/{album}/{title}.mp3`, returns relative path.

### Existing extensions

- **TrackRepository** — add `get_by_id(track_id, user_id)` and `update(track)` methods. The existing port has only `add` and `list_for_user`.

## Trigger wiring

Narrow: the HTTP router checks `created=True` and spawns a background task calling `AcquireTrackAudio.execute(track_id, user_id)`. Not a general event bus. The use case is trigger-agnostic — moving the trigger to an event handler later requires zero changes to acquisition logic.

## Search strategy: tiered waterfall

Each tier searches, scores candidates against gates, and stops if a candidate passes.

| Tier | Search query | Platform | When used |
|------|-------------|----------|-----------|
| 1 | `ytmsearch:{isrc}` | YouTube Music | Track has ISRC |
| 2 | `ytmsearch:{title} {artist}` | YouTube Music | Always (skipped if Tier 1 matched) |
| 3 | `ytmsearch:{title} {artist} {album}` | YouTube Music | Album known, Tiers 1-2 failed |
| 4 | `ytsearch5:{title} {artist}` | YouTube | Last resort, Tiers 1-3 failed |

Top 5 candidates per tier. Stop at first tier that produces a passing candidate.

## Matching: gate-based, not score-based

No weighted composite score. No tunable threshold. Three hard pass/fail gates:

| Gate | Metric | Requirement | Notes |
|------|--------|-------------|-------|
| Title | Jaro-Winkler (normalized) | >= 0.85 | Normalization: NFKC, casefold, strip brackets + feat, collapse whitespace. Reuse `normalize_for_match`. |
| Artist | Jaro-Winkler (normalized) | >= 0.70 | Lower bar than title — channel names diverge from artist names more often |
| Duration | Absolute diff | <= 15 seconds | Skipped if track has no `duration_seconds` |

**Selection:** From candidates passing all gates, pick highest title JW. Zero candidates pass → `no_match_found`.

**ISRC special case:** Tier 1 ISRC match bypasses gates entirely (ISRC is a recording-level identifier). Duration still checked as a sanity warning (logged, not rejected).

**Design decision:** Gates were chosen over weighted scoring because:
1. No threshold to tune — gates are fixed implementation details
2. A candidate can't compensate for a wrong title with a correct duration (which weighted scoring allows and is how the legacy system downloaded wrong tracks)
3. Each gate has clear, testable meaning

## Pipeline: 6 steps with rollback

### AcquisitionContext (shared mutable state)

```
track: Track              # input — loaded from repo
candidates: list          # populated by SearchStep
selected: Candidate       # populated by SelectStep
temp_path: Path | None    # populated by DownloadStep
tagged_path: Path | None  # populated by TagStep (same file, tags written in-place)
audio_ref: str | None     # populated by StoreStep
updated_track: Track      # populated by UpdateTrackStep
```

### Steps

**Step 1: SearchStep**
- Execute: run tiered waterfall via AudioSearcher port, populate `ctx.candidates`
- Rollback: nothing (read-only)

**Step 2: SelectStep**
- Execute: apply gates to candidates, select best. If none pass, raise `NoMatchError`
- Rollback: nothing (decision-only)

**Step 3: DownloadStep**
- Execute: download selected candidate via AudioSearcher port to temp dir. `bestaudio` → FFmpeg → MP3 320kbps. Populate `ctx.temp_path`
- Rollback: delete temp file if exists

**Step 4: TagStep**
- Execute: write ID3v2.4 tags to the temp file: title, artist, album, year, track_number, album_artist, genre. Embed album art from `artwork_url` (fetch + embed). Populate `ctx.tagged_path` (same file, tagged in-place)
- Rollback: nothing (file is still temp)

**Step 5: StoreStep**
- Execute: move tagged file to permanent location via AudioStore port. Populate `ctx.audio_ref`
- Rollback: delete stored file if exists

**Step 6: UpdateTrackStep**
- Execute: load Track from repo, create new Track instance with `audio_ref` + `AcquisitionStatus.READY`, persist via repo update. Populate `ctx.updated_track`. Emit `TrackAcquisitionCompleted` event (logged).
- Rollback: revert Track to PENDING + null `audio_ref` via repo update

### Pipeline runner

Executes steps sequentially. On exception:
1. Calls `rollback()` on completed steps in reverse order
2. Loads Track, creates new instance with `FAILED` + `failure_reason`
3. Persists via repo
4. Emits `TrackAcquisitionFailed` event (logged)
5. Does NOT re-raise — the background task completes cleanly

## Metadata enrichment

The `CreateTrackRequest` (backend DTO) and the mobile `toCreateTrackRequest()` mapper need to carry additional fields from the discovery `SearchResult.extras`:

| Field | Source in extras | Enables |
|-------|-----------------|---------|
| `isrc` | `extras.isrc` | Tier 1 ISRC search (near-guaranteed match) |
| `year` | `extras.year` | ID3 tagging, future disambiguation |
| `genre` | `extras.genre` | ID3 tagging |
| `album_artist` | `extras.album_artist` | ID3 tagging |

These fields already exist on the Track aggregate (added by `import-legacy-library`). The change is wiring them through the save flow.

## Error handling

### AcquisitionStatus state machine

```
PENDING ──(pipeline succeeds)──> READY
PENDING ──(pipeline fails)────> FAILED
```

No `ACQUIRING` intermediate state in v1. Tracks remain `PENDING` during acquisition. Deferred transitions (`FAILED → PENDING` for retry) are future specs.

### failure_reason

New field on Track aggregate: `failure_reason: str | None`. Null for PENDING and READY. Values are human-readable strings, not an enum:

- `no_match_found` — all tiers exhausted, no candidate passed gates
- `download_error: {detail}` — yt-dlp failure
- `storage_error: {detail}` — filesystem write failure
- `tagging_error: {detail}` — ID3 tag failure

Persisted in `tracks` table (new nullable TEXT column), returned on wire in `TrackResponse`.

### Server restart

In-progress background tasks are lost on restart. Tracks remain PENDING indefinitely. Acceptable for pre-launch; startup reconciliation is a post-launch enhancement.

## Telemetry

Domain events logged by the use case:

- `TrackAcquisitionStarted(track_id, user_id, search_strategy, has_isrc)`
- `TrackAcquisitionCompleted(track_id, user_id, duration_ms, tier_matched, candidate_title, candidate_channel, expected_title, expected_artist, file_size_bytes)`
- `TrackAcquisitionFailed(track_id, user_id, reason, search_query, candidates_evaluated, best_rejected_score)`

The completed event carries enough detail for manual accuracy spot-checks.

## File storage

Path: `{MUSIC_DIR}/{user_id}/{artist}/{album}/{title}.mp3`
- `MUSIC_DIR` from environment config (e.g., `/mnt/oci-music`)
- Filenames sanitized (strip `<>:"/\|?*;`, collapse whitespace)
- No collision risk: `UNIQUE(user_id, dedup_key)` constraint ensures one Track per (title, artist, album) per user
- `audio_ref` stores the relative path after `MUSIC_DIR/` (e.g., `uuid/Artist/Album/Title.mp3`)

## Dependencies

- yt-dlp Python package (pip)
- FFmpeg (system dependency, already on OCI instance from legacy)
- mutagen or eyed3 (for ID3 tagging — pip)
- `MUSIC_DIR` env var added to platform/config.py Settings
