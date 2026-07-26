# Acquisition context — router

The yt-dlp pipeline that finds, ranks, downloads, verifies, tags and stores audio for a saved Track. A *customer* of catalog: it consumes `catalog/domain.Track` and its `MarkReady`/`MarkFailed`/`RevertToPending` invariants rather than owning an aggregate — hence no `domain/` folder here. Correctness definition, selection model, invariants and open tensions: `ARCHITECTURE.md`.

Layout:

- `service/` — `acquire.go` (`Execute`, `resolveIdentity`, `CoreSteps`, `failureReason`), `pipeline.go` (the `Step` chain), `steps_*.go`, `matching.go` (candidate scoring), `scheduler.go`, `joblog.go`, `retry_admission.go` (`RetryAdmission`, `ReacquireAdmission`, the shared `cooldownGate`). `Execute` acquires; `ExecuteReplace` swaps the audio of an already-ready track.
- `service/eval/` — the offline selection harness: `case.go` (golden format + embedded `goldens/`), `ports.go` (case-driven fakes), `harness.go` (`Run` over the real `CoreSteps`), `report.go` (per-class accuracy, baseline gating). Goldens are `goldens/selection.json` (ranking and the audio gates) and `goldens/verification.json` (identity, fingerprint corroboration, tolerance edges). Driven by `cmd/acquisitioneval`.
- `service/registry.go` — `SourceRegistry`: equal-treatment fan-out across every `AudioSource`, merge by URL, `Fetch` routed back to the owning source.
- `ports/` — `AudioSearcher`, `AudioTagger`, `AudioProber`, `AudioWriter`, `TrackRepository`; `source.go`'s `AudioSource` / `FindRequest` / `SearchQueries`; `recording.go`'s `RecordingResolver` / `RecordingIdentity`; `identify.go`'s `AudioIdentifier` / `RecordingMatch`.
- `adapters/` — `handler/` (retry + reacquire endpoints), `ytdlp/` (searcher, prober, `Source` — text search), `ytmusic/` (`Source` — catalog-resolved by video id), `streamrip/` (`Source` per service — catalog-resolved via the `rip` CLI), `id3/` (tagger), `chromaprint/` (fpcalc + AcoustID identifier), `discoverybridge/` (`RecordingResolver` over discovery's search service).

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
- Manual re-acquire is the mirror: ready-with-audio only, its own 60s cooldown.
- A replace never marks a track failed and never deletes the audio it preserved.
- A replace excludes the stored `AudioSourceURL`, or ranking returns the same file.
- `complete` is the only call site that advances job counters.
- Never reach discovery directly — go through `adapters/discoverybridge`.
- Identity resolution is fill-only and fail-open: it never overwrites saved metadata and never fails acquisition.
- Candidate ranking stays deterministic — `sort.SliceStable` plus `breakTie`, never `sort.Slice`.
- Never add a variant word bank to matching; fix the algorithm.
- A selection change must re-clear `cmd/acquisitioneval` against its committed baseline.
- Every source is equal in the fan-out; reputation lives in ranking, never in per-source gatekeeping.
- A `Resolved` candidate skips the identity gate and ranks first — never the audio gates.
- Never fabricate channel or title metadata to push a candidate past the identity gate; mark it `Resolved` instead.
- A catalog source resolves from `RecordingIdentity.SourceFor`, never by its own text search.
- An unresolvable catalog source returns no candidates, never an error.
- Streamrip services stay config-gated; an unset `STREAMRIP_SERVICES` disables them without failing startup.
- One source failing never fails the search; only every source failing does.
- Tighten the duration gate only when the length is corroborated (`Identity.Duration > 0`).
- The fingerprint only ever corroborates: it marks `verified` on a cluster hit and never rejects.
- Set provenance only through `Track.SetAcquisitionProvenance`, and only from `AcquisitionContext.Provenance()`.

Why each rule exists: `okf/backend/acquisition/index.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
