# Acquire audio from SoundCloud

> Spec for `acquire-soundcloud` — version 1, drafted 2026-06-21.
> Authors: solo + Claude.
> Status: Code-complete, UNVERIFIED end-to-end (2026-06-21). NOT "done" — the unit
> tests mock Download and Store, so no real SoundCloud download or OCI store has
> been proven. Done requires a live run that acquires the correct full audio and
> plays it back. Two increments written: (1) dual-engine searcher;
> (2) **direct-source acquisition** — the primary correctness path, added after
> the spec on the insight that the pipeline almost always downloads *something*,
> so the real problem is *wrong* audio, not *no* audio. See "Direct-source path"
> below. Live download/OCI path is unverifiable in the dev env (no yt-dlp/network/
> OCI); covered by seams. See plan.md.

## Direct-source path (built 2026-06-21, post-spec)

The headline correctness fix, agreed in conversation after this spec. When a saved
result carries the exact SoundCloud URL the user discovered, acquisition downloads
**that exact track** (`Download → Tag → Store → Update`) instead of re-searching by
title/artist — which can grab a wrong reupload. SoundCloud is the only discovery
provider that is also a yt-dlp-downloadable source (Deezer/iTunes/MusicBrainz are
DRM/metadata), so "download the exact thing" = the SoundCloud path by construction.

- **Flow (pass-through, no migration):** the discovered URL rides
  `CreateTrackRequest.source_url` → the create handler → `Schedule(userId, trackId, sourceURL)`
  → `Execute(..., sourceURL)`. If `isDirectAcquireURL(sourceURL)` (a soundcloud.com
  host), the direct chain runs; on any failure it falls back to the dual-engine
  search ("last resort"). Mobile sets `source_url` from the result's SoundCloud
  `SourceRef` in `toCreateTrackRequest`.
- **Still deferred:** persisting the source URL on the Track. It is not persisted,
  so retries / stream-triggered re-acquisition (which only have a `trackId`) fall
  back to search. Persisting would need a schema migration (human-reviewed per the
  go-database rule) and is the only thing standing between "first acquire is exact"
  and "every acquire is exact."

---


## Problem

The audio-acquisition pipeline (search → select → download → tag → store →
mark-ready) only searches **YouTube** (`ytsearch5:`). YouTube does not index the
unreleased / leaked / underground long tail — the exact tracks SoundCloud is the
*only* source for, and the reason this self-hosted owned-library product values
SoundCloud at all. So when a user saves one of those tracks, acquisition searches
YouTube, finds nothing that passes the match gates, and the track lands in
`FAILED` — unacquirable — even though its full audio is one `scsearch:` away.

## User value

Saving an underground / leaked track that exists on SoundCloud now actually
acquires its audio into the owned library (status → `ready`, streamable),
instead of failing. Mainstream tracks are unaffected — they keep resolving to
the same YouTube source as before.

## Scope tier / MVP cut

Right-size to the project stage. **Default for this solo, pre-launch app: the minimal tier.**

- **Minimal (ship this):** the acquisition searcher queries **both** `ytsearch5:`
  and `scsearch5:` for each generated query, merges the candidates (dedup by
  URL), and hands the union to the existing selection step. Everything
  downstream is unchanged: yt-dlp already downloads SoundCloud URLs natively, so
  Download / Tag / Store / mark-ready / the background scheduler / the
  `AcquisitionStatus` state machine are reused as-is.
- **Deferred to post-launch:**
  - **Direct-permalink acquisition** — carrying the SoundCloud URL the user
    *discovered* (already in the search result's `SourceRef`) through
    save → acquire so the pipeline downloads that exact track instead of
    re-searching by metadata. Higher value/precision, but needs a Track/flow
    (and likely schema) change — its own spec.
  - SoundCloud-title cleanup heuristics (stripping `[FREE]`, `(prod. …)` noise
    before the identity gate).
  - Running the two engine searches concurrently.
  - Encrypted-HLS / Go+ special handling (yt-dlp handles standard SC streams;
    the underground long tail is never Go+-gated — see `docs/providers/soundcloud.md` §5.6).
  - A config flag to disable SoundCloud search.
- **Justified exceptions:** none.

The Acceptance criteria below cover the **minimal tier only**.

## Acceptance criteria

Each one is testable. Each one will become at least one automated test. The
searcher's subprocess call is put behind an injectable runner seam so the
dual-engine merge logic is unit-testable without invoking yt-dlp.

1. **AC#1 (both engines queried)** — Given a search query, when the searcher
   runs, then it issues **both** a `ytsearch5:` and an `scsearch5:` search for
   that query (asserted via the injected runner recording the search specs).
2. **AC#2 (candidates merged)** — Given YouTube returns candidate set A and
   SoundCloud returns candidate set B for a query, when the searcher returns,
   then the result is the union of A and B.
3. **AC#3 (dedup by URL)** — Given the two engines return overlapping URLs, when
   merged, then each URL appears once.
4. **AC#4 (one engine fails, other succeeds)** — Given the `ytsearch5:` run
   errors but `scsearch5:` succeeds (or vice-versa), when the searcher returns,
   then it returns the succeeding engine's candidates and no error (a single
   engine failure does not fail acquisition).
5. **AC#5 (both fail)** — Given both engine runs error, when the searcher runs,
   then it returns an error (no candidates).
6. **AC#6 (SoundCloud fills a YouTube gap — selection)** — Given a track whose
   only identity-passing candidate is a SoundCloud upload (no qualifying YouTube
   Topic channel), when selection runs over the merged set, then the SoundCloud
   candidate is selected. (Exercises that `SelectBestCandidate` already routes a
   non-Topic SoundCloud candidate through the metadata-rank path — confirming the
   existing gates need no change.)
7. **AC#7 (mainstream unaffected)** — Given a track with a qualifying YouTube
   `- Topic` candidate AND a SoundCloud candidate in the merged set, when
   selection runs, then the YouTube Topic candidate is still selected
   (Topic-first preference is preserved).

## Out of scope

- **Direct-permalink acquisition** (deferred tier above) — the high-precision
  path that uses the discovered SoundCloud URL; this spec is metadata-re-search
  only.
- Any change to the selection gates, the download/tag/store steps, the state
  machine, the scheduler, or the Track schema.
- The yt-dlp `client_id` / cookie / js-runtime plumbing (already configured for
  the existing YouTube path; SoundCloud rides the same yt-dlp invocation).
- Discovery-side SoundCloud (search/browse/related) — separate, already shipped.

## Design considerations

Vault: [vault: wiki/concepts/Anti-Corruption Layer Pattern.md]. The
`YtDlpAudioSearcher` is the ACL between the external world (yt-dlp/YouTube/
SoundCloud) and the internal `AudioCandidate` model. Adding SoundCloud is a
second source *inside* that same ACL — the boundary and the internal model are
unchanged, which is exactly why nothing downstream moves.

High-level approach (not implementation detail — that's the plan):

- This is an **adapter-layer** change in the `acquisition` context
  (`adapters/ytdlp/searcher.go`). No domain, no port, no service change.
- The `AudioSearcher.Search(ctx, query)` **contract is unchanged** — it still
  takes a plain query; the adapter now fans that query out to two yt-dlp search
  engines internally and merges.
- **Selection already handles it**: `SelectBestCandidate` prefers YouTube
  `- Topic` channels first and only falls to metadata-rank (where a SoundCloud
  uploader competes) when no Topic candidate passes the identity gate. So a
  SoundCloud candidate can *fill a gap* but never *displace* a good YouTube
  match — adding it is safe by construction (AC#6/AC#7).

## Dependencies

- **Bounded contexts**: `acquisition` (existing), `catalog` (Track / AudioStore —
  unchanged).
- **Other features**: `acquire-track` (the pipeline this extends),
  `oci-object-storage-v1` (the store — unchanged).
- **External services**: yt-dlp with SoundCloud support (already used for the
  discovery SoundCloud fallback and for YouTube download); SoundCloud api-v2 is
  *not* needed here — yt-dlp's own SoundCloud extractor handles `scsearch:` and
  SC-URL download.
- **Library/framework additions**: none.

## Risks / open questions

- **Risk**: SoundCloud titles carry noise (`[FREE]`, `(prod. X)`, reposts) that
  can drag `identityScore` below the 60 gate, so a real match is rejected —
  mitigation: acceptable failure mode for v1 (better to fail than acquire the
  wrong audio); title-cleanup is a deferred follow-up if it bites.
- **Risk**: doubling yt-dlp subprocess calls per acquisition (4 queries × 2
  engines) lengthens the background job — mitigation: acquisition is async with a
  10-min ceiling and runs once per track; acceptable. Concurrency is deferred.
- **Risk**: a SoundCloud upload that is a re-upload/bootleg of a mainstream track
  could win when YouTube has no Topic channel — mitigation: the identity gate +
  duration score still apply; and for genuinely mainstream tracks a Topic channel
  usually exists and wins first.
- **Open question**: should SoundCloud be skipped when a YouTube Topic candidate
  already qualifies (save the subprocess)? — deferred; would couple SearchStep to
  selection. Resolve only if subprocess cost bites.

## Telemetry

- **Log events**: the searcher already logs `acquisition.search_query` /
  `acquisition.search_query_results`; extend with the engine (`youtube` /
  `soundcloud`) and per-engine candidate count. `candidate_selected` already logs
  the chosen channel/source — a SoundCloud win is visible there.
- **Metrics**: acquisition success rate split by selected source (YouTube vs
  SoundCloud) — the signal that proves this unlocked the long tail. Deferred-but-noted.
- **Alerts**: none pre-launch.

## Related

- `docs/specs/acquire-track/spec.md` — the pipeline this extends.
- `docs/specs/oci-object-storage-v1/spec.md` — the store (unchanged).
- `docs/providers/soundcloud.md` §5.6 / §8 (Unit D) — the capability and the
  full-vs-preview / HLS notes this consumes.
- `[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]`.
