---
title: "feat: Artist identity resolution v3 — eliminate thresholds"
type: feat
status: active
date: 2026-06-19
origin: docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md
---

# feat: Artist identity resolution v3 — eliminate thresholds

## Summary

Replace the threshold-based `FilterContamination` with a multi-layer identity resolution pipeline that makes binary same-artist/different-artist decisions per album. The pipeline runs R2 (MB reverse-lookup) → R3 (iTunes/Discogs cross-search) → R3b (profile constraints) → R3c (ISRC registrant fingerprint), returning each album as confirmed, contamination, or unknown. Prerequisites: extract rich metadata from existing provider adapters to build the artist identity profile that feeds disambiguation and constraint checks.

---

## Problem Frame

The v2 contamination filter uses a mismatch-count threshold (`>= 2`) that fails for niche artists where only one cross-reference source is available. API probing during the brainstorm proved that all known contamination albums for "Che" escape the current filter. (see origin: `docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md`)

---

## Requirements

- R1. Build rich artist identity profile from all providers
- R2. MBID reverse-lookup per unconfirmed album (definitive)
- R3. Cross-provider album search on iTunes/Discogs (bidirectional — confirms or flags)
- R3b. Identity profile constraint checks (temporal, type, genre — binary, individually decisive)
- R3c. ISRC registrant fingerprint (with 24h suspect safeguard)
- R4. Optimistic include for unknown albums
- R5. Replace FilterContamination with IdentityResolver
- R6. Cache identity resolution results (30d confirmed/contamination, 24h unknown)
- R7. Rate-limit aware batch processing
- R8. Squeeze provider metadata extraction

**Origin acceptance examples:** AE1 (Che clean), AE2 (Aurora clean), AE3 (Banks clean), AE4 (new album survives), AE5 (no hardcoded values)

---

## Scope Boundaries

- Audio fingerprinting (deferred until acquisition pipeline)
- ML/model-based classification (deferred until team grows)
- Frontend changes (backend-only)
- New provider integrations (better use of existing ones)
- SoundCloud-exclusive release discovery (deferred to separate spec)
- Last.fm tag comparison (Q3 — deferred; iTunes + MB + Discogs + ISRC covers enough)

---

## Context & Research

### Relevant Code and Patterns

- Current contamination filter: `services/go-api/internal/discovery/service/discography_filter.go` — `FilterContamination()` + `DiscographyFilterInput`
- Current orchestration: `services/go-api/internal/discovery/service/get_artist_content.go` — `validateAndFilterDiscography()` + `applyDiscogsEnrichment()`
- Ports: `services/go-api/internal/discovery/ports/ports.go` — `AlbumValidator`, `DiscographyEnricher`, `ArtistIdentity`, `DiscogsArtistInfo`
- MB adapter: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — `ValidateArtistAlbums()`, `ResolveArtistIdentity()`, rate limiter
- Discogs adapter: `services/go-api/internal/discovery/adapters/providers/discogs.go` — `ResolveDiscogsArtist()`, `FetchArtistReleases()`, rate limiter
- iTunes adapter: `services/go-api/internal/discovery/adapters/providers/itunes.go` — search only, no album-lookup
- Deezer adapter: `services/go-api/internal/discovery/adapters/providers/deezer.go` — `GetArtistAlbums()`, track detail for ISRCs
- Cache pattern: `services/go-api/internal/discovery/adapters/cache/discogs_cache.go` — Redis, SHA256 keys, sentinel values, TTL split
- Shared normalization: `services/go-api/internal/shared/textnorm/normalize.go` — `NormalizeForMatch()` (extracted in today's audit)
- App wiring: `services/go-api/internal/app/app.go` — singleton adapters, functional options injection

### Institutional Learnings

- `docs/solutions/2026-06-07-extras-merge-provider-priority.md` — provider priority for extras merging

---

## Key Technical Decisions

- **ArtistIdentityProfile as a domain type**: New struct in `domain/` carrying MBID, birth year, area, type, genre set, known ISRC registrants. Built incrementally from multiple providers. Not an aggregate — a read-model assembled at query time and cached.
- **IdentityResolver as a port**: New port interface with a single method `ResolveAlbumIdentity(ctx, profile, album) → (verdict, error)`. Each resolution layer (R2, R3, R3b, R3c) is an implementation detail of the service, not a separate port.
- **iTunes album search via existing adapter**: Extend the iTunes adapter with an `SearchAlbumByTitle(ctx, title) → (artistName, genre, error)` method. No new provider integration — just a new method on the existing adapter.
- **ISRC extraction via Deezer track detail**: Fetch ISRC from first track of each album using existing Deezer adapter's HTTP client. One call per album.
- **Cache key**: `discovery:identity:v1:{sha256(artistName|albumTitle)[:16]}` — per-artist-album pair as resolved during grilling (Q2).
- **R3c suspect state**: Cache value includes a `firstSeen` timestamp. On re-evaluation, if `firstSeen > 24h ago` and still not confirmed → promote to contamination.

---

## Open Questions

### Resolved During Planning

- **Caching granularity**: Per-artist-album pair (resolved during grilling)
- **Last.fm tags**: Deferred — iTunes + MB + Discogs + ISRC covers known contamination (resolved during grilling)
- **Resolution order**: Definitive first (R2, R3), then heuristics (R3b, R3c) — confirmed during grilling to avoid deferred verdicts when definitive answers are available

### Deferred to Implementation

- **Q1 measurement**: Exact unconfirmed album counts for Aurora, Banks, Common — measure during verification, not planning
- **iTunes rate limits**: iTunes search API has no published rate limit but may throttle. Monitor during integration testing; add backoff if needed
- **Genre compatibility definition**: Which Deezer genre_ids are "compatible" with which identity profile genres. Build the mapping from real data during implementation

---

## Implementation Units

- U1. **Extract provider metadata (R8)**

**Goal:** Enrich provider adapters to extract all available metadata fields into `SearchResult.Extras`, building the data foundation for identity resolution.

**Requirements:** R8, feeds R1

**Dependencies:** None

**Files:**
- Modify: `services/go-api/internal/discovery/adapters/providers/deezer.go`
- Modify: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go`
- Modify: `services/go-api/internal/discovery/adapters/providers/itunes.go`
- Modify: `services/go-api/internal/discovery/adapters/providers/discogs.go`
- Test: `services/go-api/internal/discovery/adapters/providers/deezer_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/musicbrainz_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/itunes_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/discogs_test.go`

**Approach:**
- Deezer: extract `genre_id` on album results in `mapDeezerResult()`. Deezer album response already returns `genre_id` — just map it to `Extras["genre_id"]`
- MusicBrainz: extract `tags[]`, `area`, and `type` from artist search responses in `mapMBArtist()`. MB already returns these fields in JSON — parse and store in Extras
- iTunes: extract `primaryGenreName` from search results in the existing mapping function. Already in the iTunes API response
- Discogs: populate `Genre` and `Country` fields in `DiscogsArtistInfo` from `fetchArtistDetail()` response. Discogs already returns these

**Patterns to follow:**
- Existing `extras["mbid"]`, `extras["isrc"]`, `extras["nb_fan"]` pattern in each adapter's mapping functions

**Test scenarios:**
- Happy path: Deezer album result includes `genre_id` in extras
- Happy path: MB artist result includes `area`, `type`, `tags` in extras
- Happy path: iTunes result includes `primaryGenreName` in extras
- Happy path: Discogs artist info includes `Genre` and `Country` when available
- Edge case: Missing fields (nil/empty) don't crash — extras key is simply absent

**Verification:**
- All existing tests pass
- New test cases verify metadata extraction for each provider

---

- U2. **Define ArtistIdentityProfile domain type and IdentityResolver port (R1, R5)**

**Goal:** Define the core domain type for the artist identity profile and the port interface for identity resolution. Remove the old `DiscographyFilterInput` type.

**Requirements:** R1, R5

**Dependencies:** None (domain types are standalone)

**Files:**
- Create: `services/go-api/internal/discovery/domain/identity.go`
- Modify: `services/go-api/internal/discovery/ports/ports.go`
- Test: `services/go-api/internal/discovery/domain/identity_test.go`

**Approach:**
- `ArtistIdentityProfile` struct: `MBID`, `DiscogsID`, `BirthYear`, `Area`, `ArtistType` (person/group), `GenreCluster` (set of genre strings), `KnownISRCRegistrants` (set of strings), `Disambiguation`
- `AlbumVerdict` enum: `Confirmed`, `Contamination`, `Suspect`, `Unknown`
- `IdentityResolver` port interface: `ResolveAlbums(ctx, profile ArtistIdentityProfile, albums []SearchResult) ([]AlbumResolution, error)` where `AlbumResolution` carries `{Album, Verdict, Reason, Layer}`
- Extend existing `ArtistIdentity` to include `Area`, `ArtistType`, `GenreTags` — or supersede it with `ArtistIdentityProfile` (cleaner)
- Add `ProfileBuilder` methods to incrementally add signals from different providers

**Patterns to follow:**
- Existing `ArtistIdentity` struct in `ports/ports.go` — extend the pattern
- Existing enum pattern (`ResultKind`, `Confidence`) in `domain/types.go` for `AlbumVerdict`

**Test scenarios:**
- Happy path: Profile builder accumulates signals from multiple providers
- Happy path: GenreCluster set intersection works correctly
- Edge case: Empty profile (no signals from any provider) has sensible defaults
- Edge case: ISRC registrant extraction from valid and malformed ISRC strings

**Verification:**
- Domain types compile and tests pass
- Profile builder produces correct output from mixed provider signals

---

- U3. **Identity resolution cache (R6)**

**Goal:** Redis cache for per-artist-album identity verdicts with split TTLs and suspect-state tracking.

**Requirements:** R6, R3c (suspect safeguard)

**Dependencies:** U2 (domain types for `AlbumVerdict`)

**Files:**
- Create: `services/go-api/internal/discovery/adapters/cache/identity_cache.go`
- Test: `services/go-api/internal/discovery/adapters/cache/identity_cache_test.go`

**Approach:**
- Key: `discovery:identity:v1:{sha256(artistName|albumTitle)[:16]}`
- Value: JSON `{verdict, reason, layer, firstSeen, resolvedAt}`
- TTLs: 30 days for confirmed/contamination, 24 hours for unknown/suspect
- `firstSeen` timestamp enables the R3c safeguard: suspect entries older than 24h can be promoted to contamination on re-evaluation
- Get/Set interface following `discogs_cache.go` pattern: nil-client early return, slog warning on errors

**Patterns to follow:**
- `services/go-api/internal/discovery/adapters/cache/discogs_cache.go` — key format, TTL split, sentinel values, error handling

**Test scenarios:**
- Happy path: Set confirmed verdict, Get returns confirmed with correct TTL
- Happy path: Set unknown verdict, Get returns unknown
- Happy path: Set suspect verdict with firstSeen, Get returns suspect with timestamp
- Edge case: Nil client returns cache miss, no panic
- Edge case: Expired entry returns cache miss
- Integration: Suspect entry older than 24h is distinguishable from fresh suspect

**Verification:**
- Cache roundtrip works for all verdict types
- TTL differentiation works correctly

---

- U4. **MB reverse-lookup layer (R2)**

**Goal:** For each unconfirmed album, search MB for the album+artist and check whether it's credited to our artist's MBID or a different one.

**Requirements:** R2

**Dependencies:** U1 (MB metadata extraction), U2 (domain types)

**Files:**
- Modify: `services/go-api/internal/discovery/adapters/providers/musicbrainz.go`
- Test: `services/go-api/internal/discovery/adapters/providers/musicbrainz_test.go`

**Approach:**
- New method on `MusicBrainzAdapter`: `LookupAlbumArtist(ctx, artistName, albumTitle string, profile ArtistIdentityProfile) → (verdict AlbumVerdict, creditedMBID string, error)`
- Search MB: `release-group:"AlbumTitle" AND artist:"ArtistName"`
- If results: compare credited artist MBID against profile MBID. Different = contamination, same = confirmed, ambiguous = unknown
- Use profile (area, type, genre) to disambiguate when multiple MB results match the artist name
- Respects existing rate limiter (1 req/sec)

**Patterns to follow:**
- Existing `fetchReleaseGroups()`, `fetchArtistMatches()` in `musicbrainz.go`
- `textnorm.NormalizeForMatch()` for title comparison

**Test scenarios:**
- Happy path: Album found, credited to same MBID → confirmed
- Happy path: Album found, credited to different MBID → contamination
- Happy path: No MB results → unknown (fall through)
- Edge case: Multiple results, profile disambiguates correctly
- Edge case: MB returns 429 / error → unknown (don't block on MB failure)
- Covers AE1: Known contamination albums return "no results" (validated by API probing)

**Verification:**
- Unit tests with HTTP fixtures pass
- Rate limiter not violated

---

- U5. **iTunes cross-provider search layer (R3)**

**Goal:** Search iTunes for unconfirmed albums to check if they're credited to a different artist or the same artist with compatible/incompatible genre. Bidirectional: can confirm or flag contamination.

**Requirements:** R3

**Dependencies:** U1 (iTunes metadata extraction), U2 (domain types)

**Files:**
- Modify: `services/go-api/internal/discovery/adapters/providers/itunes.go`
- Test: `services/go-api/internal/discovery/adapters/providers/itunes_test.go`

**Approach:**
- New method on iTunes adapter: `LookupAlbum(ctx, albumTitle, artistName string, profile ArtistIdentityProfile) → (verdict AlbumVerdict, error)`
- Search: `https://itunes.apple.com/search?term={albumTitle}&entity=album&limit=5`
- Match logic: find result where `collectionName` matches (normalized). Then:
  - `artistName` differs → contamination
  - `artistName` same + `primaryGenreName` incompatible with profile genre cluster → contamination
  - `artistName` same + `primaryGenreName` compatible → confirmed
  - No matching result → unknown

**Patterns to follow:**
- Existing iTunes search in `itunes.go`
- Genre compatibility: compare iTunes `primaryGenreName` against profile `GenreCluster` using set membership

**Test scenarios:**
- Happy path: Album found, different artist name → contamination (covers AE1: LOTTO DREAMS by Mr. E.L.Y)
- Happy path: Album found, same name, incompatible genre → contamination (covers AE1: Tšernobõl by Che/Rock)
- Happy path: Album found, same name, compatible genre → confirmed
- Happy path: Album not found → unknown (covers: Samsonite not on iTunes)
- Edge case: Multiple iTunes results for same title — pick best match by normalized artist name
- Edge case: iTunes API error → unknown (don't block on iTunes failure)

**Verification:**
- Unit tests with HTTP fixtures covering all three outcomes
- Bidirectional behavior verified: confirms AND flags

---

- U6. **Profile constraints + ISRC registrant layers (R3b, R3c)**

**Goal:** Implement the fallback identity checks: temporal impossibility, artist type mismatch, genre cluster incompatibility (R3b), and ISRC registrant fingerprinting with 24h suspect safeguard (R3c).

**Requirements:** R3b, R3c

**Dependencies:** U1 (metadata), U2 (domain types), U3 (cache for suspect state)

**Files:**
- Create: `services/go-api/internal/discovery/service/identity_constraints.go`
- Modify: `services/go-api/internal/discovery/adapters/providers/deezer.go` (ISRC fetch method)
- Test: `services/go-api/internal/discovery/service/identity_constraints_test.go`
- Test: `services/go-api/internal/discovery/adapters/providers/deezer_test.go`

**Approach:**
- R3b constraints as pure functions: `CheckTemporalImpossibility(profile, album)`, `CheckArtistTypeMismatch(profile, album)`, `CheckGenreIncompatibility(profile, album)` — each returns `(violated bool)`
- R3c: new method on Deezer adapter `FetchTrackISRC(ctx, trackID string) → (isrc string, error)` — fetch ISRC from first track
- R3c logic: extract registrant (chars 3-6 of ISRC), compare against profile's `KnownISRCRegistrants` set
- R3c safeguard: if registrant mismatches and this is first encounter → return `Suspect`. On re-evaluation (cache `firstSeen > 24h`), promote to `Contamination`
- Each constraint is individually decisive — any single violation = contamination (R3b) or suspect (R3c)

**Patterns to follow:**
- Existing `extractYear()` in `discography_filter.go` for temporal check
- Existing Deezer HTTP client pattern for ISRC fetch

**Test scenarios:**
- Happy path: Album year before birth year → temporal impossibility → contamination
- Happy path: ISRC registrant differs from known set → suspect (first encounter)
- Happy path: ISRC registrant differs + firstSeen > 24h → contamination (re-evaluation)
- Happy path: ISRC registrant matches known set → no violation
- Edge case: No birth year known → temporal check skips
- Edge case: Empty genre cluster → genre check skips
- Edge case: Malformed ISRC → check skips
- Edge case: Album has no tracks (empty tracklist) → ISRC check skips
- Covers AE1: Samsonite (CH7812 ≠ J842), Gallos Ciegos (FZ62 ≠ J842), Kiss Me in the Sky (K6K2 ≠ J842)
- Covers AE4: New album with unknown registrant → suspect, not immediately removed
- Covers AE5: No hardcoded artist names or thresholds

**Verification:**
- All constraint checks are pure functions with deterministic tests
- ISRC extraction works with real ISRC format
- Suspect → contamination promotion requires 24h elapsed

---

- U7. **IdentityResolver service orchestrator (R5, R7)**

**Goal:** Wire all layers into the `IdentityResolver` service that replaces `FilterContamination`. Orchestrate R2 → R3 → R3b → R3c with short-circuiting, rate-limit awareness, and caching.

**Requirements:** R5, R7, R4

**Dependencies:** U2 (ports/types), U3 (cache), U4 (MB layer), U5 (iTunes layer), U6 (constraints + ISRC)

**Files:**
- Create: `services/go-api/internal/discovery/service/identity_resolver.go`
- Modify: `services/go-api/internal/discovery/service/get_artist_content.go` — replace `validateAndFilterDiscography` + `applyDiscogsEnrichment` with identity resolver call
- Delete: `services/go-api/internal/discovery/service/discography_filter.go`
- Test: `services/go-api/internal/discovery/service/identity_resolver_test.go`
- Modify: `services/go-api/internal/discovery/service/discography_filter_test.go` → delete or migrate to new tests

**Approach:**
- `IdentityResolverService` struct holds: MB adapter, iTunes adapter, Discogs adapter, Deezer adapter (for ISRC), identity cache, and profile builder
- `Resolve(ctx, profile, albums) → []AlbumResolution`: for each album:
  1. Check cache → return cached verdict if found
  2. Run R2 (MB) → if definitive, cache and return
  3. Run R3 (iTunes, Discogs) → if definitive, cache and return. If confirmed, add ISRC registrant to profile
  4. Run R3b (constraints) → if any violated, cache and return contamination
  5. Run R3c (ISRC) → if mismatch, return suspect (or contamination if re-evaluation)
  6. Default: unknown
- Process albums concurrently across providers but respect per-provider rate limits
- R2 and R3 run for each album in parallel (MB and iTunes have independent rate limits)
- Short-circuit: once any layer makes a definitive decision, skip remaining layers for that album
- Profile builder runs once before album resolution loop, assembling from existing MB/Discogs/Deezer data

**Execution note:** Build the orchestrator incrementally — wire one layer at a time and verify with integration-style tests before adding the next.

**Patterns to follow:**
- Existing `enrich()` concurrency pattern in `search_music.go` (semaphore + WaitGroup)
- Functional options for dependency injection (existing pattern on `SearchMusicService`)

**Test scenarios:**
- Happy path: Album confirmed by MB (R2) → short-circuits, no iTunes/ISRC check
- Happy path: Album not in MB, confirmed by iTunes (R3) → short-circuits
- Happy path: Album not in MB or iTunes, temporal impossibility (R3b) → contamination
- Happy path: Album escapes R2/R3/R3b, ISRC mismatch (R3c) → suspect on first encounter
- Happy path: Suspect album on re-evaluation (>24h) → contamination
- Happy path: Album passes all checks → unknown, kept
- Integration: Full pipeline for "Che" test data — confirmed albums survive, contamination removed, new release kept
- Edge case: MB rate limit causes timeout for one album → that album gets unknown, others still processed
- Edge case: All providers fail → all albums unknown (graceful degradation)
- Covers AE1: LOTTO DREAMS (R3-iTunes), Tšernobõl (R3-iTunes), Samsonite (R3c-ISRC), Gallos Ciegos (R3c-ISRC)
- Covers AE4: Brand-new album → unknown → kept below confirmed
- Covers AE5: No thresholds in orchestration logic

**Verification:**
- Integration test with mocked providers verifies complete pipeline
- `FilterContamination` and `DiscographyFilterInput` are deleted — no threshold code remains
- `get_artist_content.go` calls identity resolver instead of old validation flow

---

- U8. **App wiring and integration (R1, R5)**

**Goal:** Wire the IdentityResolver into the app's dependency graph, replacing the old AlbumValidator/DiscographyEnricher injection.

**Requirements:** R1, R5

**Dependencies:** U7 (identity resolver service)

**Files:**
- Modify: `services/go-api/internal/app/app.go`
- Modify: `services/go-api/internal/discovery/service/get_artist_content.go` (constructor options)

**Approach:**
- Create `IdentityResolverService` in `app.go` with injected adapters: sharedMB, sharedDiscogs, iTunes adapter (existing), Deezer adapter (existing), Redis client (for identity cache)
- Replace `WithAlbumValidator` + `WithDiscogsEnricher` options on `GetArtistContentService` with a single `WithIdentityResolver` option
- Remove old wiring for `DiscographyEnricher` from artist content service (Discogs enrichment moves into identity resolver)
- Ensure sharedMB and sharedDiscogs singletons are still shared (rate limiter state must be consistent)

**Patterns to follow:**
- Existing `app.go` wiring pattern: config check → create adapter → inject via functional options

**Test scenarios:**
- Test expectation: none — wiring changes verified by integration tests in U7 and end-to-end verification

**Verification:**
- `go build ./...` succeeds
- All existing tests pass
- Server starts and serves discography requests with new identity resolution

---

## System-Wide Impact

- **Interaction graph:** `GetArtistContentService.GetAlbums()` is the sole entry point. The identity resolver replaces `validateAndFilterDiscography()` + `applyDiscogsEnrichment()`. No other callers are affected.
- **Error propagation:** Identity resolution failures degrade gracefully to "unknown" verdict (optimistic include). No album is removed due to an API error — only due to positive identity mismatch evidence.
- **State lifecycle risks:** The 24h suspect → contamination promotion for R3c requires the cache to store `firstSeen` timestamps. Clock skew between app restarts is acceptable (24h window is coarse).
- **API surface parity:** The HTTP response from `/artists/{provider}/{externalId}/albums` is unchanged — same `ContentFetchResponseDTO`. The filtering happens server-side before serialization.
- **Integration coverage:** End-to-end test with mocked providers should verify: confirmed albums appear first, contamination removed, unknown albums appear after confirmed. This crosses service → adapter → cache layers.
- **Unchanged invariants:** Search pipeline (`SearchMusicService.Execute()`) is not affected. Artist collapse, disambiguation, enrichment, and ranking remain unchanged. Only the discography content fetch path changes.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| MB/iTunes API calls add 30-60s cold-cache latency | Cache with 30d TTL. UX: show confirmed albums immediately, resolve rest in background (R6) |
| iTunes may throttle under heavy per-album lookups | Monitor during integration testing. Add exponential backoff if needed. Deferred to implementation |
| ISRC registrant is not standardized — edge cases exist (compilation albums, label changes) | R3c is last resort and has 24h suspect safeguard. False positives self-correct when R2/R3 confirm |
| Deleting FilterContamination removes the existing safety net | U7 integration tests must cover all cases the old filter covered before deletion |
| Rate limiter contention between search and identity resolution (shared MB adapter) | Shared singleton already serializes; identity resolution runs on discography load, not search. Low contention risk |

---

## Sources & References

- **Origin document:** [docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md](docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md)
- API probing evidence: documented in origin under R2, R3, R3c sections
- Related prior plan: [docs/plans/2026-06-19-001-feat-artist-identity-v2-plan.md](docs/plans/2026-06-19-001-feat-artist-identity-v2-plan.md)
- Audit fixes (same session): NormalizeForMatch extracted to `services/go-api/internal/shared/textnorm/`
