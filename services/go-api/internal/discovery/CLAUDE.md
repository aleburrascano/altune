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
