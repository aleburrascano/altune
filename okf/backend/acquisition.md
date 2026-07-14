---
type: Bounded Context
title: Acquisition
description: The yt-dlp-backed pipeline that finds, ranks, downloads, verifies, tags, and stores audio for a saved Track, with retry/scheduling and rich telemetry.
resource: services/go-api/internal/acquisition/
tags: [bounded-context, hexagonal, go-api, pipeline, ytdlp, background-jobs, retry]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

Acquisition is a customer of the catalog context: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants rather than owning its own aggregate. Its own domain types (`ports/ports.go`: `AudioCandidate`) and pipeline-scoped types (`service/pipeline.go`: `TrackRef`, `Candidate`, `AcquisitionContext`) live in `service/`, not a `domain/` folder — this context is process/orchestration-heavy, not aggregate-heavy.

**Pipeline** (`service/pipeline.go`): a `Step` interface (`Name`, `Execute`, `Rollback`) run in order by `RunPipeline`, which rolls back completed steps (reverse order, 30s timeout) on failure and wraps the failing step in a `StepError{Step, Err}`. `AcquireTrackAudioService.buildSteps` (`service/acquire.go`) assembles: `SearchStep` → `SelectStep` → `DownloadStep` → `TagStep` → `StoreStep` → `UpdateTrackStep`.

- **Search** (`step_search.go`): builds several query variants (ISRC, title+artist, +album, +"audio") and calls `AudioSearcher.Search`. The yt-dlp adapter (`adapters/ytdlp/searcher.go`) fans each query to two search engines (`ytsearch5:` YouTube, `scsearch5:` SoundCloud), merges/dedupes by URL, and only fails if every engine errors.
- **Select** (`step_select.go` + `service/matching.go`): `rankCandidates` scores every candidate with `identityScore` (title/artist fuzzy match via `textnorm.TokenSortRatio`, penalizing title-only matches), gates out anything below `identityMin` (60), splits survivors into Topic-channel (`isTopicChannel`) vs other, and orders Topic-channel first (then artist-match, then feature-match via `extractFeaturedArtists`/`featureMatch`, then identity), other by identity/feature/metadata-rank.
- **Download** (`step_download.go`): walks the ranked list (capped at `maxVerifyAttempts=4`), downloading each candidate and — when an `AudioProber` is wired and the track has an expected duration — rejecting downloads whose probed duration falls outside `durationWithinTolerance` (15s or 7% slack), falling through to the next candidate. `Rollback` deletes the temp file.
- **Tag** (`step_tag.go`): writes ID3v2.4 tags (title/artist/album/year/track#/album-artist/genre) via `bogem/id3v2`; tagging failure is logged and swallowed, never fails the pipeline.
- **Store** (`step_store.go`): builds a sanitized `userId/artist/album/title.mp3` audio ref and calls `AudioWriter.Store`; rollback deletes the orphaned object.
- **UpdateTrack** (`step_update_track.go`): calls `Track.MarkReady(audioRef)` + `SetDuration` and persists; rollback reverts to pending.

**Ports** (`ports/ports.go`): `AudioSearcher` (search+download), `AudioProber` (ffprobe-backed duration check, `adapters/ytdlp/prober.go`), `AudioWriter` (store-side subset of catalog's `AudioStore`), `TrackRepository` (narrowed get/update).

**Scheduling** (`service/scheduler.go`): `BackgroundAcquisitionScheduler` runs jobs on goroutines gated by a semaphore channel, dedups in-flight work per track via `sync.Map.LoadOrStore`, and maintains an in-memory `AcquisitionStatus` (queued/running/recent jobs, succeeded/failed counters, recent failures) for the operator console — reset on restart, no persistence. `Shutdown` cancels and drains via `WaitGroup` with a timeout.

**Retry** (`service/retry_admission.go`, `adapters/handler/retry_handler.go`): `RetryAdmission` rate-limits manual re-acquisition to one per track per 60s cooldown (in-memory map, opportunistically pruned); `RetryHandler` only allows retrying tracks in `AcquisitionFailed` state, returning 429 if the cooldown is active.

`failureReason` (`acquire.go`) maps internal step errors to a small, stable, client-safe vocabulary ("no matching audio found", "audio download failed", "audio storage failed", "audio acquisition cancelled") — the full error chain is logged, never stored on the track or returned over the wire.
