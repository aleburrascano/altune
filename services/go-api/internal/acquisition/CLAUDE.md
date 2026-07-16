# Acquisition context — router

The yt-dlp pipeline that finds, ranks, downloads, verifies, tags, and stores audio for a saved Track. A *customer* of catalog: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants rather than owning an aggregate — hence no `domain/` folder here (deliberate: this context is orchestration-heavy, see the concept doc).

Invariants:

- The pipeline is a `Step` chain (`service/pipeline.go`); on failure completed steps roll back in reverse order. A new step must implement `Rollback` honestly or leave orphans.
- Tagging failure is logged and swallowed — it must never fail the pipeline.
- `failureReason` maps step errors to a small client-safe vocabulary; the raw error chain is logged, never stored on the track or sent over the wire.
- Manual retry is admission-gated: only `AcquisitionFailed` tracks, one per track per 60s.

Knowledge base: `okf/backend/acquisition.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
