# Discovery context — local rules & status

Covers the **discovery** bounded context: the search pipeline (`service/`),
providers (`adapters/providers/`), and the `cmd/discoveryeval` eval harnesses.
For how to build/run the API see `services/go-api/CLAUDE.md`. Pipeline shape +
ranking key: `ARCHITECTURE.md` (read it before auditing the pipeline).

## Current status (2026-06-24)

Pipeline is the rebuilt **Merge → Rank** core (ADR-0007 strangler addendum).
Correctness is solid; there is no known ranking bug. Measured against cloned
prod data (1897 tracks) via `discoveryeval`:

- **Exact `"artist title"` eval — 100% top-3.** This is the real product bar
  (what users actually type) and it's met.
- **Bare single-token title eval — ~81% top-3.** A stress metric with an inherent
  ambiguity ceiling: a bare title ("Hello", "Scorpion") legitimately surfaces the
  *famous* track over the user's niche owned one, and discovery is not
  personalized. Not a bug.
- **Merge — 0% under-merge / 0% over-merge.** Collapses everything it provably can.
- **Correction — 93% recall / 100% precision.**

**Popularity is currently NOT a live ranking signal.** `SearchResult.Popularity`
is never populated in the search path (the rebuild dropped the wiring), so it
stays 0 and ranking ties break on **multi-source → RRF**, not popularity. Wiring
it back (log of Deezer rank/fans, SoundCloud plays, Last.fm listeners) was tried
and **reverted (2026-06-24)**: a same-sample A/B showed it *regressed* the
bare-title eval (top-3 81%→75%) because this is a personal niche library — see
`docs/plans/2026-06-24-001-test-discovery-eval-harness-program-plan.md`. Don't
redo it naively. (The "Popularity > multi-source" line in old docs is stale
intent, not current reality.)

## Eval harnesses — the regression gate (`cmd/discoveryeval`)

Real pipeline, in-process, live providers + DB/Redis. Nightly/on-demand, **not**
per-commit. Every gated mode: run → gate metrics vs committed `baselines.json` →
print attributed-failure slices → exit 2 on regression.

```bash
cd services/go-api
go run ./cmd/discoveryeval -mode eval                 # ranking, exact corpus (top-3 bar)
go run ./cmd/discoveryeval -mode eval -corpus hard    # bare single-token titles (the hard case)
go run ./cmd/discoveryeval -mode merge                # under/over-merge
go run ./cmd/discoveryeval -mode correction           # synthetic-typo recall/precision (offline)
go run ./cmd/discoveryeval -mode diversity            # reshaping cost (rule on vs off)
go run ./cmd/discoveryeval -mode signal-a|signal-b    # coverage gaps / provider imbalance
go run ./cmd/discoveryeval -mode health|consensus     # report-only gauges (never gate)
go run ./cmd/discoveryeval -mode artwork -limit N -random  # artwork coverage: % of library artists resolving identity/provider/name/blank + attributed gaps (flush Redis first for a cold read)
go run ./cmd/discoveryeval -mode detail               # artist detail/discography: same-name contamination (gated =0) + recall + metadata, over seeded fractured identities (no DB needed)
# re-baseline (explicit, reviewed): measures value + empirical noise margin
go run ./cmd/discoveryeval -mode eval -update-baselines -noise-runs 3
```

Useful flags: `-limit N` (cap corpus), `-concurrency N`, `-top-k 3`,
`-query "X"` (dump top results for one query), `-json path`.

## Testing discipline (load-bearing)

- **Position, not presence.** "Is the right answer in the top 10" is far too weak.
  Gate is top-3 via `discoveryeval`; the canonical spot-checks below assert top-3.
- **A/B on an IDENTICAL deterministic sample** (`-limit`, no `-random`). Deltas
  across random samples are noise — a plausible change once looked "+7pp" that the
  same-sample A/B revealed as a *regression*. This is how the popularity attempt
  was caught.
- **No hardcoded workarounds.** If a query ranks wrong, fix the algorithm — never
  add a word to a bank or special-case list. They rot immediately.
- **Question stages before adding them.** Each session tends to add ranking layers.
  Ask: "if I remove this stage, do the positioning tests still pass?"
- **Log the math.** `LOG_LEVEL=debug` → `search.ranking` logs show per-result
  scores. **Verify provider responses directly** (`curl api.deezer.com/...`) — don't
  assume from memory.

### Canonical spot-checks (top-3, blended)

```
"Humble"              → top-3 contains the Kendrick track "HUMBLE."
"Scorpion"            → top-3 contains a Drake "Scorpion" result
"Bohemian Rhapsody"   → top-3 contains a Queen result
"Drake" / "Bad Bunny" → top-3 contains the artist
"Blinding Lights"     → top-3 contains the Weeknd track
"Kendrick Lamar Humble" → top-3 contains "HUMBLE." by "Kendrick Lamar"
```

Test blended AND filtered (`kinds=album`, `kinds=track`). `track>album>artist` is
a held-in-reserve, non-query-fit tiebreak for strict-#1 polish — not the gate.

## Known pipeline reality (current rebuilt core)

- **Rank order:** continuous relevance (token-sort similarity) → popularity
  (currently inert, see above) → multi-source → RRF (k=60) → stable title tiebreak.
  No relevance bands, no kind tiers, no intent contract (those were query-fit and
  were purged in the rebuild).
- **Merge:** identifier (ISRC / album-UPC / MBID) → cross-provider identity bridge →
  exact canonical title+subtitle. No fuzzy threshold. Deezer/MusicBrainz/Apple
  (ISRC+UPC) and Last.fm (mbid on all search kinds, decoded 2026-07-23) carry
  identifiers; other cross-provider pairs merge by exact canonical title.
- **Albums have no provider popularity data** (Deezer returns `nb_fan=0`); they
  compete on multi-source/RRF only.
- **Pre-correction disabled** (it rewrote valid queries from vocabulary pollution);
  post-correction (zero-results-only) is sufficient.

## Adapter invariants (`adapters/`)

### Reverse-engineered providers — the fragile tier

Spotify, Amazon Music, and Deezer's lyrics path ride undocumented internal endpoints, **against those providers' ToS**, accepted deliberately because the other providers already give graceful degradation if they break.

- **Spotify is the most fragile integration here.** The search persisted-query hash and the `spotify_content` operation hashes **rotate without notice** whenever Spotify redeploys its web player; a stale hash must surface as a real error rather than an empty result. The TOTP secrets in `spotify_totp.go` also rotate periodically and cannot be derived analytically — they must be re-scraped. The access token's cached expiry is skewed a minute early so a request never starts against a token that expires mid-flight, and the TOTP counter needs Spotify's *server* time, not local time.
- `deezer_lyrics` uses `pipe.deezer.com` + `auth.deezer.com` with an anonymous JWT. Expect rotation (401 → re-bootstrap **once**), and **never block the hot search path on it**.
- SoundCloud rotates how its `client_id` is embedded, which periodically breaks the bootstrap.
- Every one of these self-healing 401 handlers shares one rule: **a second 401 handler must not wipe the fresh credential the first one just obtained** — re-bootstrap only if the credential still matches the one the request failed with.
- `ytmusic_client` breaks by *drift*, not by error: the request keeps succeeding while the response shape changes (the parser knew `musicShelfRenderer`). When results go empty, probe the raw response before assuming an outage.

### Artwork chain

Identity-only resolvers (those that fetch by a bridged id) are tried first and **never name-search**. Only after they miss may the chain fall back to a name search, and it **must label that result provisional, not identity**, so a same-name guess can never masquerade as a proven-identity image. `spotify_artwork`'s `Resolve` is a deliberate no-op for exactly this reason — oEmbed needs a Spotify URL, so it resolves only by proven id and a same-name artist ("Che") can never inherit another's face. Image-format handling is best-effort: a format change degrades to 320px, never to broken.

The `ArtworkCache` repeats the port's overwrite guard: a weaker result never replaces a real, higher-confidence image.

### Persistence

- **A lookup failure must never break the search path** — degrade to a miss. A cache write failure must never fail the search.
- `entity_identity_repo`: persisting a narrow xref (say only `{deezer}`) **must not erase** the `{spotify, discogs}` edges an earlier write learned.
- Malformed JSON payloads are **skipped per row**, never allowed to raise a `22P02` that bricks the whole batch. Event ids are NULL for fire-and-forget events so they never collide on the idempotency key.
- `related_tracks_repo`'s **cross-user scan (no `user_id` filter) is deliberate, not a bug** — it reads only non-identifying track metadata (title/artist/album/artwork URL), no user id and no PII. Do not "fix" it by adding a user filter.

### Provider quirks worth keeping

- Rate limiting is **per-provider, not uniform** — MusicBrainz reserves the strictest budget. MusicBrainz caches only successful results: errors and timeouts are never cached, so a transient failure doesn't freeze. A cancelled request must not keep sleeping out its backoff.
- Last.fm's artist MBID is **deliberately not mapped** onto the result MBID — a stale Last.fm mbid that disagrees with MusicBrainz would corrupt identity. Last.fm's enrichment reads the body even on a non-200, because a 4xx-delivered miss otherwise looked transient and the negative cache never engaged.
- iTunes/Apple ids can never bridge-match: the bridge is xref-gated and MusicBrainz url-relations never carry an Apple/iTunes id. iTunes title-suffix matching is **suffix-only** — a mid-title match ("Bad - Remix Album") must not trip it.
- MusicBrainz enrichment resolves strictly or returns nothing ("nothing to enrich"), **never a fuzzy guess**, and ties break alphabetically so tests and the UI agree on order.
- SoundCloud tracks otherwise never merge with other providers, and an EP track must never also render as a top-level single.
- YouTube Music has no SLA, so a slow or hung request must not block a search indefinitely. Its by-name gathering is for artwork only — **never wire it into the search path**.

## Service invariants (`service/`)

### Search orchestration (`search.go`, `pipeline.go`, `fanout`)

- The exported `Merge` / `RankWith` / `Reshape` composition exists **outside** `Service` so the Mission Control re-run ranks with the same core production does and can never diverge. `RankExplain` keeps each result's scoring math and is for the re-run inspector only — a pure read, never on the live search path. Display-enrichment stages stay on `Service.mergeRankEnrich`: **they fill fields, they do not decide order.**
- **A panicking provider adapter must not kill the process** — the fan-out records the panic and continues. Each goroutine writes only its own slot, so output follows the fixed provider order and never completion order.
- The durable identity persist runs **off the request path** on a detached, bounded context: it must add no latency to search and must outlive the request. A wrong MB url-relation must not fuse two same-name artists in the durable store. Vocabulary trim is best-effort — a trim failure must not fail the search.
- Telemetry is best-effort throughout: it never blocks the request, outlives request cancellation (`WithoutCancel` plus its own timeout), recovers from panics, and logs rather than surfacing. `pipelineVersionV2` stamps every rebuilt-pipeline event so ML training data is separable.
- One search event per **search**, not per page. Scrolling must not log a second search or re-ingest vocabulary. A client holding an offset from a larger slate must degrade, not fail.
- **Exploration never mutates the cached list** — it shuffles a copy and is inert at rate 0. The organic (pre-shuffle) top is captured *first*, because vocabulary learning must ingest the organic ranked top, not the shuffled one.
- The engagement-join signature is computed **before** disambiguation fills fields; a signature computed after would flap between searches and never rejoin.

### Merge (`merge.go`)

Identifier (ISRC / album-UPC / MBID) → cross-provider identity bridge → exact canonical title+subtitle. There is **deliberately no fuzzy threshold**. UPC is advisory — different pressings of one album carry different barcodes — so unlike MBID it never blocks; it merges only when MBIDs don't conflict (both empty, one empty, or equal). A later merge (e.g. a UPC one after an ISRC one) **must not downgrade an identity-proven entity's stamped identity**. For a bridge to fire, one side must carry an `Xref`: two native ids alone are same-provider duplicates, not a cross-provider bridge.

### Rank (`rank.go`)

Continuous relevance → popularity (inert) → multi-source → RRF → stable title tiebreak. Deliberately **no relevance bands, no popularity-dominance window, no kind tiers** — those were query-fit and were purged in the rebuild. The multi-source comparison is deliberately **unconditional**: gating it to kind-difference made `rankLess` non-transitive. Flag-gated stages (`isLowConfidenceTail`, behavioral) have both predicates unset by default, so they never fire until enabled. Anything rank-affecting **must re-clear `discoveryeval`**.

Tail demotion never flags the corroborating providers (Deezer/iTunes/MusicBrainz), and is uniform so it cannot reorder within the demoted set.

### Artwork fill (`artwork_fill.go`)

The one rule everything else serves: **a name-searched image must never masquerade as identity.** When MusicBrainz never answered a search, the durable `IdentityStore` supplies the identity rather than gambling on a name lookup — the operator console then reads `durable-identity`, which is the fix made visible. There is no same-name fallback: a placeholder is correct, a stranger's face frozen as identity is not. A provisional image is labelled as such so a real identity image can replace it later. Per-result artwork diagnostics surface in Mission Control, never on the public wire.

### Consensus and detail (`consensus.go`, `detail_identity.go`, `release_*.go`)

- Detail resolution is keyed on **provider ids, never names**, so a same-name artist ("Che") can never bleed into another's discography. The resolved identity is never narrower than the input. `get_album_tracks` guards against returning a different artist's same-titled album.
- **Partial verdicts must never be cached.** If the context is cut mid-MB-validation, verdicts are incomplete; a transient MB failure degrades to serving the un-vetoed union but must not freeze a potentially contaminated result. Cache keys carry the seed identity so two same-name artists never share an entry.
- `identity_verify` is **fail-open**: never drop an edge on a fetch failure or empty result.
- A single iTunes fetch is one source and is never reported as "confirmed on multiple providers". A provider carrying only a year must never mask another's full date, and artwork URL + source are tagged together so `ArtworkSource` cannot describe the wrong URL.
- The MB anchor has minimum thresholds (`mbAnchorMinReleaseGroups`, `mbVerifyMinTitles`) precisely so an **incomplete MB discography can never drop a real release**.
- Providers under-label `record_type` (iTunes never set it), which is why release bucketing exists rather than trusting the field.

### Enrichment and degradation

Every detail-open enricher returns an **empty enrichment and a nil error** — the endpoint always answers 200, never a surfaced failure. A provider returning `(nil, nil)` still honors the envelope contract's non-nil `Items`. A failure must not poison the positive cache, and a negative result is cached so a repeat doesn't re-run resolution. A `SuggestByPrefix` failure during token correction degrades to "no prefix match" and keeps going rather than aborting correction. Behavioral snapshots are published by **swapping in a new map**, never editing a published one — callers must not mutate what they read, and a failed refresh must not clobber the last good snapshot.

`query_clean` operates on the **original** text, never a lowercased copy: lowercasing can change byte offsets.

### Eval substrate (`service/eval/`)

A gate is a **relative drop below a committed baseline, never an absolute floor**. A metric with no committed baseline is never a regression — the first run establishes it, and baselines only move on an explicit operator `--update-baselines`. A regression strictly inside the noise margin is invisible **by design**. Failure slices only group the failure log; they never touch ranking, and no failure-mode taxonomy is maintained. Diversity's benefit metric (top-K concentration drop) is report-only and **never gated** — you gate the cost of a policy, not the policy itself. Entities the search never finds are a *coverage* miss, not a merge miss. The eval package never imports `service`, so it stays a pure testable core.

## Domain value objects (`domain/`)

The enrichment types (`MBEnrichment`, `DeezerEnrichment`, `DeezerLyrics`, `LastFmEnrichment`, `DiscogsEnrichment`) are immutable **live read surfaces** fetched on detail-open and never persisted. Each covers all applicable kinds with one shape; kind-specific fields are simply zero when not applicable. They divide by authority: MusicBrainz owns identity + curated genres + artwork, Discogs owns credits + styles, Last.fm owns listening behavior (popularity, folksonomy tags, bio, similar artists), and Deezer owns audio fields plus album liner data. **Lyrics are the one axis no other audited provider carries** — sourced from Deezer's internal `pipe.deezer.com` GraphQL (anonymous-JWT), separate from the public-API `DeezerEnrichment` surface, and availability is per-track and region-dependent.

Invariants that are easy to break:

- Every `Empty*Enrichment` constructor returns **non-nil** slices/maps so the wire mapping never emits a null list. All graceful-degradation paths (no id, no data for this region, provider error) return that value, not a zero struct.
- `MBEnrichment.IsZero` is false whenever an MBID is present, even if every other field is empty — **the MBID alone unlocks artwork**.
- `DeezerEnrichment.Featured` is consumed by the "Featuring" row, not the Deezer detail section, so it is deliberately **excluded from `IsZero`**: a featured-only enrichment still has no section to render.
- Deezer zero values are meaningful absences, not data: BPM `0` = unknown (Deezer reports 0 for many tracks), ReplayGain `0` = absent (it's a volume-normalization value, never a display field).
- `EventType`'s zero value is the **unknown sentinel**, so an uninitialized event is never silently valid, and `"unknown"` is `String()`'s output, not a wire value. `ClientSubmittable` allows only the interaction types — `search_performed` and `results_shown` are server-emitted envelope events and must be rejected at the `POST /events` boundary.
- `InteractionEvent.SearchId` is the keystone join key (the UUID of the originating `search_performed`) and lives in a **real column**, not the payload; it's empty for events with no originating search, e.g. a play from the library. `EventId` is the client-minted idempotency key for label-critical events (`library_add`, `wrong_album`) delivered via the outbox. `ClientOccurredAt` is client-recorded time versus `OccurredAt`/`received_at` server time, and is zero for server-minted events.
- `FeaturedArtist` serialization lives in the domain (`ToExtrasMap` / `FeaturedArtistsToExtras` / `FeaturedArtistsFromExtras` / `FeaturedArtistFromMap`) rather than at each call site, because the domain owns the contract. Empty ids are **omitted** so absence stays distinguishable from a zero id, an empty slice returns nil so the key is omitted rather than emitted empty, and the parser tolerates the numeric variance a JSON round-trip introduces (`int64` vs `float64`, and `[]any` rather than `[]map[string]any`). `RoleFeatured` is the only role populated in v1; the field is reserved so producer/writer credits can arrive without a shape change.
- `ArtistIdentityProfile` is a query-time **read model**, not an aggregate.

## Ports (`ports/`)

The artwork ports encode a strict precedence that must not be flattened:

- `IdentityArtworkResolver` is an optional capability for resolvers that can fetch by a **bridged provider id** instead of by name. The chain tries these **before** any name-based resolver, and a resolver implementing it is treated as identity-only — it never name-searches.
- `TaggingArtworkResolver` is the service-side port: `ResolveWithIdentityTagged` resolves strictly by proven identity, `ResolveTagged` is the name path. `Source` is `""` when nothing resolved, and is recorded as `SearchResult.ArtworkSource` for per-provider coverage visibility (a resolver names itself via the optional `SourcedArtworkResolver`).
- **`ArtworkCache.Set` has a confidence-guard invariant**: a write at *lower* confidence than an existing *positive* entry (non-empty URL) must be a **no-op**. A name guess or a later resolution failure must never clobber a proven-identity image. Equal or higher confidence overwrites. `ArtworkConfidence` is what lets the cache treat a proven-identity image as authoritative (long TTL) and a name-searched one as provisional (short TTL, re-checked so it can upgrade once identity is learned).
- `MBIDIndex` is **cache-only** — never a MusicBrainz call on the search path. A detail-open's strict name resolution memoizes `(kind, nameKey) → mbid`; the search path reads it so the MBID-keyed artwork tier (Cover Art Archive / Fanart.tv) fires on search cards too. A miss degrades to the provider's own thumbnail.
- `IdentityStore` is the durable, MB-independent counterpart to the in-flight identity bridge: `PersistBridges` records what the merge learned when MusicBrainz answered, so a later search where MusicBrainz is absent (rate-limited, circuit open) still resolves identity-first instead of guessing by name. It is **keyed on stable provider ids, never names**, so same-name entities ("Che") cannot inherit each other's identity. Postgres is the source of truth with a Redis read-through in front; implementations are nil-safe no-ops when unset.

## Key files

- `ARCHITECTURE.md` — flow diagram + ranking key
- `service/search.go` — `Service` orchestrator (fanOut + mergeRankEnrich)
- `service/merge.go` / `service/rank.go` / `service/diversity.go` — the Merge→Rank→reshape core
- `service/enrich/` — detail-open enrichers (Deezer, Last.fm, Discogs, lyrics) + the `CachedLookup` read-through helper
- `service/eval/` + `cmd/discoveryeval/` — the offline regression harnesses (eval, merge, correction, diversity, health, coverage signals)
- `adapters/providers/` — one file per provider (Deezer, Last.fm, MusicBrainz, iTunes, SoundCloud, YouTube/YT Music, Discogs, Genius, Amazon Music, Apple Music, Spotify). iTunes (`itunes.go`, plain Search API) and Apple Music (`applemusic.go`, official Catalog API via the anonymous web-player token) are separate adapters over the identical catalog/ids — only Apple Music sits in the search fan-out (richer: ISRC, composer credits, lyrics flag); iTunes stays wired for the artwork chain, album consensus, and content lookups. Spotify (`spotify.go` + `spotify_totp.go` + `spotify_token.go`) rides the same anonymous web-visitor path Spotify's own player uses (TOTP-gated access token, ported from the open-source SpotifyScraper project's reverse-engineering, plus a separate client-integrity token) — materially more fragile than the other providers here (Spotify actively rotates the TOTP secrets and the search persisted-query hash); accepted anyway since the other three providers already give graceful degradation if it breaks.

## Knowledge base

`okf/backend/discovery/index.md` indexes the nine discovery subsystem concept docs — read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
