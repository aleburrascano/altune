# Acquisition context — router

The yt-dlp pipeline that finds, ranks, downloads, verifies, tags and stores audio for a saved Track. A *customer* of catalog: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants rather than owning an aggregate — hence no `domain/` folder here.

Layout:

- `service/` — `acquire.go` (`Execute`, `CoreSteps`, `failureReason`), `pipeline.go` (the `Step` chain), `steps_*.go`, `matching.go` (candidate scoring), `scheduler.go`, `joblog.go`, `retry_admission.go`.
- `ports/` — `AudioSearcher`, `AudioTagger`, `AudioProber`, `AudioWriter`, `TrackRepository`.
- `adapters/` — `ytdlp/` (searcher, prober), `id3/` (tagger), `handler/` (retry endpoint).

## Rules

- A new `Step` must implement `Rollback` honestly or it leaves orphans.
- Tagging failure is logged and swallowed — it must never fail the pipeline.
- Never store or return a raw error chain; map it through `failureReason` first.
- Never tag a non-MP3 container — ID3v2 corrupts anything that isn't MP3.
- Never treat `ProbeDuration` as proof the audio decodes; that is `ValidateDecodable`'s job.
- Never block acquisition on an unavailable validator — skip validation instead.
- Never skip re-acquisition on a transient existence-check error; fall through and repair.
- Never let a SoundCloud candidate displace a qualifying YouTube Topic match.
- Never fail a search because one engine failed.
- Never add "with" to the featured-artist pattern.
- Never match features against a normalized title — read the raw one.
- Change the pipeline's shape only in `CoreSteps`, never in one caller.
- `CleanupTemp` takes the parent of `TempPath`, never `TempPath` itself.
- Manual retry stays admission-gated: `AcquisitionFailed` only, one per track per 60s.
- `complete` is the only call site that advances job counters.

Why each rule exists: `okf/backend/acquisition/index.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
