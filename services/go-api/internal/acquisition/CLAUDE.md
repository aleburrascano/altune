# Acquisition context — router

The yt-dlp pipeline that finds, ranks, downloads, verifies, tags, and stores audio for a saved Track. A *customer* of catalog: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants rather than owning an aggregate — hence no `domain/` folder here (deliberate: this context is orchestration-heavy, see the concept doc).

Invariants:

- The pipeline is a `Step` chain (`service/pipeline.go`); on failure completed steps roll back in reverse order. A new step must implement `Rollback` honestly or leave orphans.
- Tagging failure is logged and swallowed — it must never fail the pipeline.
- `failureReason` maps step errors to a small client-safe vocabulary; the raw error chain is logged, never stored on the track or sent over the wire.
- Manual retry is admission-gated: only `AcquisitionFailed` tracks, one per track per 60s. Retries carry no source URL (the request is by trackId), so acquisition re-searches by metadata.
- `CoreSteps` is the single definition of the search → select → download → tag → store sequence, shared by the production service (via `buildSteps`, which appends its own `UpdateTrackStep`) and the reacquire CLI commands (which update `audio_ref` themselves and stop before that step). One place decides the pipeline's shape so the two callers cannot drift; `buildsteps_test.go` pins the assembly and order.
- `CleanupTemp` removes the *parent* of `TempPath`, not `TempPath` itself — `DownloadStep` creates one temp dir per download attempt via `os.MkdirTemp`. Exported so the reacquire CLI, which replays the pipeline outside `Execute`, cleans up identically.

## Search and selection

- Acquisition **always re-searches by metadata**. The old direct-download path (fetching a saved SoundCloud permalink verbatim) was removed because SoundCloud's public stream is often only a ~30s preview.
- `searchEngines` fans each query to YouTube (mainstream catalogue) and SoundCloud (the unreleased/leaked/underground long tail YouTube doesn't index). Selection is **Topic-channel-first**, so a SoundCloud candidate only fills a gap where no qualifying YouTube Topic match exists — it never displaces a good YouTube result. One engine failing is tolerated; the search fails only if every engine fails.
- `featuredRe` deliberately excludes "with" — it mangles real titles like "Stuck with U", the same reason `textnorm` dropped it. `extractFeaturedArtists` reads the **raw** title because `NormalizeForMatch` strips bracketed segments, so by the time `identityScore` compares titles the `(feat. X)` is gone and the matcher is blind to features.
- `featureMatch`: when a track names featured artists, a candidate must mention every one in its raw title, otherwise it's a different recording — the solo cut or official video that the duration-blind identity score cannot tell apart. A track with no feature imposes no requirement. Title-only matches are penalized because without artist context the match is ambiguous ("Die Hard" vs Kendrick's "DIE HARD"). Spaces are stripped when matching channels so spaceless VEVO/official channels ("TheWeekndVEVO") still match.
- Duration tolerance is the larger of an absolute slack and a fraction of expected length: the same recording from any source runs the same length within a few seconds (intro/outro or silence trimming), so a larger gap means a different recording.

## Verification and tagging

- **`ValidateDecodable` exists because `ProbeDuration` is metadata-only.** A file with a valid header but corrupt samples passes duration verification and fails here — the exact defect that shipped undecodable m4a files. If ffmpeg itself can't run (missing binary, timeout), validation is *skipped* rather than blocking acquisition on an unavailable validator, mirroring `ProbeDuration`'s fail-open stance.
- **ID3v2 is MP3-only.** The tagger prepends an ID3 block at byte 0, which is correct for MP3 but corrupts any other container — an m4a/MP4 must start with `ftyp`, and the shifted bytes invalidate its sample-offset table. Non-MP3 files are skipped entirely and carry their metadata in the DB.
- `reconcileForReacquire`: a ready track whose audio file still exists is a no-op skip; a ready track with a missing file, or a previously failed track, is reverted to pending. A **transient existence-check error falls through to re-acquire** rather than skipping, so a possibly-missing file is never left unrepaired.
- Acquisition emits a server-authoritative `track_acquisition_started` event so the client seeds its download UI and flips a re-acquired ready/failed track back to pending, instead of depending on the optimistic save or the poll (regression F7/F8).

## Job telemetry

`jobLog` is the operator console's view: current queued/running jobs, running succeeded/failed totals, and a bounded ring of recent terminal outcomes. `complete` is the single call site advancing all three, so they cannot drift. Failed jobs ride the same recent ring carrying their reason — there is no separate failure ring. `JobRecord.State` is one of the `Job*` constants; `Stage` is the current pipeline step name. The `jobReporter` is always resolved through `jobReporterFrom`, which returns a no-op when none is wired, so eval and test paths that call `Execute` without a scheduler are unaffected.

Knowledge base: `okf/backend/acquisition/index.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
