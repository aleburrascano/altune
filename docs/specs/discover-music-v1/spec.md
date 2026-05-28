# discover-music-v1

> Spec for `discover-music-v1` — version 1, drafted 2026-05-27.
> Authors: solo + Claude.
> Status: Clarify-gated (spec-reviewer revise-then-approve, 4 BLOCK + relevant SHOULD-ADDRESS resolved in-pass 2026-05-27).

## Problem

The user can't find music. The legacy `music-manager` shipped a single-API search that returned wrong or missing canonical results often enough that "search accuracy" became its biggest pain — the user did not trust the search bar. Altune has no music-search surface at all yet: the only feature is `view-library`, which shows tracks the user already owns. There is no way to find an artist's discography, look up an album, surface a single release, or discover a playlist from outside the user's library — and once SoundCloud-as-leak-surface lands later, no way to find those either. Until at least *reading* the public catalog from multiple sources works, neither library-write features ("save this to my library") nor playback-from-discovery features can attach to anything.

## User value

Open the altune mobile app, tap the search surface, type a query, and see a ranked list of matching artists, albums, singles, and playlists drawn from **multiple sources unified into one list** — Deezer, MusicBrainz, SoundCloud, and Last.fm — with the system collapsing duplicates and showing a confidence signal so the user knows when the system is sure. Pasting a Deezer / SoundCloud / MusicBrainz / Last.fm URL into the search field resolves directly to that resource. The empty-state on the discover surface shows the user's last 10 searches; tapping one re-runs it. The screen handles partial-source failure (one provider unavailable), zero-result queries, and full-error scenarios as designed states, not blank screens.

## Acceptance criteria

Each AC is testable and will become at least one automated test.

1. **AC#1 (read happy path, merged ranked list)** — Given an authenticated user issues `GET /v1/discovery/search?q=the+beatles&kinds=artist,album,track,playlist&limit=25`, when all 4 providers respond within the per-source budget, then the response is HTTP 200 with body shape `{query, query_norm, results: [...], providers: [...], partial: false, cache: {hit: false, fetched_at: <iso>}}`, with `results.length ≤ 50` (post-dedup) ordered by the multi-criteria sort {confidence DESC → multi-source agreement DESC → per-source prior DESC → alpha on `(subtitle, title)`}.

2. **AC#2 (ISRC-collapse: results sharing ISRC merge into one)** — Given two providers return results with the same ISRC for the same query, when the response is built, then there is exactly one result entry with both providers in its `sources: []` array, `confidence: "high"`, and `extras.isrc` populated. Asserted by a unit test on the dedup logic using `InMemorySearchProvider`s with overlapping ISRCs.

3. **AC#3 (Jaro-Winkler match collapse, no ISRC) — separated merge predicate and confidence label.**
   - **Merge predicate**: two provider results without ISRC merge into one entry **iff** their normalized `(artist, title)` JW similarity ≥ 0.85. The per-source prior is **not** part of the merge predicate; it determines only which source's `title` / `image_url` becomes the canonical representative of the merged entry (highest prior wins; ties broken alphabetically by provider name).
   - **Confidence label** on the merged entry: `"high"` if JW ≥ 0.92, `"medium"` if JW ∈ [0.85, 0.92).
   - **Result entries with JW < 0.85 against every other entry stay separate** — they are not merged with anyone (AC#4 covers their confidence label).
   - Asserted by parameterized unit tests over a normalization+JW corpus and explicit boundary cases at JW=0.84, JW=0.85, JW=0.91, JW=0.92.

4. **AC#4 (low-confidence standalone results, intentionally separate)** — Given a provider result that has no ISRC match and JW < 0.85 against every other entry for the same query, when the response is built, then it appears as its own entry with `confidence: "low"` and `sources: [{provider: <its_provider>, ...}]` (single source). The SoundCloud leak/different-upload case is the canonical example — it must NOT be collapsed into the canonical result.

5. **AC#5 (per-source timeout + partial results)** — Given 3 of 4 providers return within 1500ms and one provider exceeds 1500ms, when the use case completes within the 2000ms overall budget, then the response is HTTP 200 with `partial: true`, the 3 successful providers' results are merged + returned, and `providers[]` contains the timed-out provider with `status: "timeout"`, `result_count: 0`, `latency_ms: 1500`. The response is **not** an error.

5a. **AC#5a (per-source non-2xx error → partial response)** — Given a provider returns an HTTP 5xx or network error within the per-source budget, when the use case completes, then the response is HTTP 200 with `partial: true`, the other providers' results are merged + returned, and `providers[]` contains the failed provider with `status: "error"`, `result_count: 0`, `latency_ms: <actual ms before failure>`. The single-provider error contributes one consecutive-failure tick toward that provider's circuit breaker (5 → trip; per AC#6).

5b. **AC#5b (per-source 429 rate-limit → partial response with distinct status)** — Given a provider returns HTTP 429 (rate-limited) within the per-source budget, when the use case completes, then the response is HTTP 200 with `partial: true`, the other providers' results are merged + returned, and `providers[]` contains the rate-limited provider with `status: "rate_limited"`, `result_count: 0`, `latency_ms: <actual>`. 429 is **distinguished from `error`** in telemetry (it indicates calling pattern, not provider health) and **does NOT** count toward the circuit breaker.

6. **AC#6 (per-source circuit breaker: source skipped while open)** — Given a provider has had 5 consecutive failures (mix of timeouts and `error` status; rate-limited does not count per AC#5b) within the last 30s and its breaker is open, when a new search query arrives, then the search use case does not call that provider; the response's `providers[]` includes that provider with `status: "circuit_open"`, `result_count: 0`, `latency_ms: 0`, and the response is still HTTP 200 with `partial: true`.

7. **AC#7 (per-source cache hit — full-warm case)** — Given a prior identical query was executed and **all four** providers' cache entries exist within their per-source TTL windows, when the same query is issued, then the response is built entirely from cache (no live provider calls), `cache.hit == true`, `cache.fetched_at` reflects the **earliest** original fetch time across the four entries, and `providers[].latency_ms == 0` for every source.

7a. **AC#7a (per-source cache mix — partial-warm case)** — Given some provider cache entries are within their TTL and others have expired, when the same query is issued, then **only the warm sources serve from cache** and the expired sources are re-fetched live (and re-cached with their per-source TTL). The merged response is built from the union. `cache.hit == true` **if at least one source served from cache**, and `cache.fetched_at` reflects the earliest cached entry's original fetch time. `providers[].latency_ms == 0` for cache-warm sources; reflects actual live-call time for refetched ones. This is the load-bearing case for the per-source TTL design — SC's 1h TTL would otherwise force a full refetch every hour even when MB / Deezer / Last.fm are still warm for days.

8. **AC#8 (cache miss → live call → cache write)** — Given no cache entries exist for the query (or all have expired), when the query executes, then live provider calls happen for every source, results are merged + returned, and the post-ACL `SearchResult` shape (not raw provider JSON) is written to Redis with the per-source TTL (`MB=24h, lastfm=12h, deezer=6h, soundcloud=1h`). Asserted by an integration test with `testcontainers` Redis.

9. **AC#9 (cache version-prefix invalidation)** — Given the cache key prefix is `discovery:v1:...` and the running deploy emits a `v2` prefix, when queries are issued, then they all cache-miss against v2 keys and re-fetch live; existing v1 entries expire naturally and the cache invalidation is non-destructive (no explicit FLUSHDB needed). Asserted by a unit test on the cache-key builder + an integration test.

10. **AC#10 (URL-paste resolution routes to per-source lookup)** — Given `q` matches one of the four supported provider URL patterns (`https?://(www\.)?(deezer\.com|soundcloud\.com|musicbrainz\.org|last\.fm)/...`), when the request is processed, then the use case skips scatter-gather, calls the matching provider's `lookup_by_url(url)` adapter method, and returns either a single-result response with `confidence: "high"` and `sources: [{provider, external_id, url}]` exactly matching the pasted URL, or an empty `results: []` with HTTP 200 if the provider returned no match.

10a. **AC#10a (URL-paste of an unsupported host falls through to text search)** — Given `q` looks like a URL (starts with `http://` or `https://`) but matches none of the four supported provider hosts (e.g., `https://spotify.com/track/...`, `https://example.com`, `https://youtube.com/watch?v=...`), when the request is processed, then the use case treats `q` as plain text and runs scatter-gather across all four providers (which will likely return zero results, hitting AC#20's zero-results state). The response is HTTP 200 — **not** 422 — to preserve URL-as-text fallback for the user.

11. **AC#11 (search history persisted on every HTTP 200 search response, including partials)** — Given an authenticated user issues a search that returns HTTP 200 (whether `partial: true` or `partial: false`), when the response is built, then a `discovery_search_history` row is persisted with `(user_id, query, query_norm, executed_at, result_clicked_signature: null)`. The persistence is **awaited before the search response is returned**, but a persist failure does not fail the search response (per AC#11a).

11a. **AC#11a (history-persist failure does not fail the search)** — Given the `discovery_search_history` insert fails (e.g., DB connection error) during a successful search execution, when the response is returned, then the search response is still HTTP 200 with the merged results, and a `search_history_persist_failed` structured log event is emitted with `user_id`, `query_norm`, and `error_type`.

12. **AC#12 (search history ring-buffer at 50)** — Given a user has 50 prior search-history rows, when a 51st search persists, then the oldest row is dropped and exactly 50 rows remain for that user. Asserted by integration test seeding 50 rows + executing one more search.

13. **AC#13 (`GET /v1/discovery/search-history` returns distinct queries, last 10 newest-first)** — Given an authenticated user has N ∈ {0, 5, 50} search-history rows, when `GET /v1/discovery/search-history` is called, then the response is HTTP 200 with body `{items: [...], total: <number of distinct query_norm values>}` where `items.length == min(distinct-count, 10)`, **distinct by `query_norm`** (collapsing rapid retries of the same query), ordered by the MAX(`executed_at`) DESC of each distinct group. Each item shape `{query: str, query_norm: str, executed_at: <iso, the most recent>}`. Note: the underlying ring-buffer of 50 rows is per-event (AC#12); the read-side endpoint de-duplicates by `query_norm` for empty-state UX.

14. **AC#14 (multi-tenancy: history isolation)** — Given two distinct users A and B both have search-history rows, when user A calls `GET /v1/discovery/search-history`, then the response contains only A's rows and none of B's. Asserted by repository integration test seeding both users explicitly with literal UUIDs.

15. **AC#15 (click-tracking endpoint persists before responding)** — Given an authenticated user POSTs `/v1/discovery/clicks` with `{query_norm, result_signature, position, confidence}`, when the request is processed, then the `discovery_search_clicks` row is persisted **before** the HTTP 202 response is returned (await-before-202, not background-task fire-and-forget) — so a follow-up integration test querying the DB immediately after observing 202 can deterministically assert row presence. The row contains user_id, query_norm, result_signature, position, confidence, and `clicked_at = now()`. The 202 (not 200) communicates "accepted; no body" — it is not a signal about persistence asynchrony.

16. **AC#16 (click-tracking idempotency — sliding-window relative to last persisted click)** — Given POST #1 to `/v1/discovery/clicks` persists at `t_0` with `(user_id, query_norm, result_signature)`, when a second POST with identical `(user_id, query_norm, result_signature)` arrives at `t_1`, then the second POST is **deduped (no new row persisted, returns 202)** if `t_1 - t_last_persisted < 60s` and **persists a new row** otherwise. Concretely: dedup is computed against the **most recent persisted row** for that `(user_id, query_norm, result_signature)` triple, not against a fixed window from the first click. Engineers implementing should check `SELECT 1 FROM discovery_search_clicks WHERE (user_id, query_norm, result_signature) = ($1,$2,$3) AND clicked_at > now() - interval '60 seconds'` before insert.

### `result_signature` definition (used by AC#11, AC#15, AC#16)

`result_signature` = a deterministic SHA-256(first 12 hex chars) of `f"{kind}|{normalize_for_match(title)}|{normalize_for_match(subtitle or '')}"`. The signature deliberately does NOT include `provider` or `external_id` so that clicks on the same canonical result via different sources collapse to one signature (matches the multi-source merge semantics of AC#2 + AC#3). The same `normalize_for_match()` used by the dedup engine produces the signature, so a click on "The Beatles - Let It Be" generates the same signature whether the user tapped the Deezer-sourced row or the MB-sourced row of the merged result.

17. **AC#17 (validation: out-of-range / missing inputs return 422)** — Given any of the inputs `{q="", limit=0, limit=201, kinds=invalid_value, kinds empty}`, when the server processes the request, then the response is HTTP 422. The response body shape is intentionally not constrained by this AC (future RFC 7807 normalization may change it).

18. **AC#18 (auth: missing / invalid bearer token returns 401)** — Given a request to any `/v1/discovery/*` endpoint without an `Authorization` header, or with an invalid / expired token (per ADR-0006's verification contract), when the server processes the request, then the response is HTTP 401 and no domain handler runs.

19. **AC#19 (tolerant-reader on malformed provider responses)** — Given a provider returns a result missing a required field (e.g., a track missing `title`), when the ACL adapter translates the response, then that single malformed result is dropped, a `provider_response_malformed` structured log event is emitted with `provider`, `kind`, and the missing field name, and the rest of the provider's results AND the overall search continue normally.

20. **AC#20 (designed mobile view states — alternative terminal renderings, not a sequence)** — For each of the five mutually-exclusive view states the Discover screen can render, when that state is active, the rendered tree contains a node with the corresponding stable `testID`:
   - **`loading`** → `testID="discover-loading"` (initial query in flight, no prior results)
   - **`empty-no-query`** → `testID="discover-empty-no-query"`, plus the last 10 distinct history items rendered as tappable rows with `testID="discover-history-row-<idx>"` (indices 0..9). Tap re-runs that query.
   - **`results`** (happy path, full or partial) → `testID="discover-results"` containing one `testID="discover-row-<result_signature>"` per merged result. If the underlying response has `partial: true`, a **sibling** node `testID="discover-partial-banner"` is rendered alongside the results list (NOT instead of it) — partial-success is not an error state; the user still sees what came back.
   - **`zero-results`** (response is HTTP 200, `partial: false`, `results: []`) → `testID="discover-zero-results"`.
   - **`full-error`** (response is HTTP 5xx, or all providers errored such that the use case itself failed) → `testID="discover-full-error"` with a `testID="discover-retry"` button that re-issues the request.

   Asserted by component test using the `_viewForState` helper pattern established in [apps/mobile/src/features/library/state.ts](../../../apps/mobile/src/features/library/state.ts).

## Out of scope

Explicit non-goals. Each is a future spec when its time comes.

- **Audio playback / preview of search results.** The `preview_url` field is reserved-null on the wire shape; `track-playback` future spec lights it up. The discovery surface returns results, not players.
- **Library-write from search.** "Save this result to my library" is a future spec (`add-result-to-library`); it consumes this spec's `sources[]` + `extras.isrc` contract but does not affect the v1 search surface.
- **Subscribe-to-artist** — same posture; future spec consuming the locked sources.
- **Pagination beyond top-N.** Cursor-based load-more is deferred to v1.x when telemetry shows users need it. v1 returns ≤50 merged results per query, full-stop.
- **Search-as-you-type.** Submit-only v1; debounced as-you-type is the locked v1.1 fast-follow per [ADR-0007](../../adr/0007-unified-music-search.md).
- **Cross-provider playlist creation / playlist editing.** Discovery surfaces playlists; it does not mutate them.
- **Provider-specific advanced query syntax** (e.g., MusicBrainz's `artist:smiths title:queen`). The v1 query is free text; per-provider query mapping is adapter-internal.
- **Romanization / phonetic matching / typo correction.** All accuracy v2 backlog items per ADR-0007. v1 normalization stops at NFKC + lowercase + diacritics-strip + bracket-strip + leading-article-strip.
- **Per-result-type pagination** (e.g., "show me more albums for this artist"). v1 returns mixed-kind results; per-kind drill-downs are a future spec.
- **Region / catalog-locking handling.** Deezer's catalog varies by country; v1 accepts the provider's regional default; future spec if it becomes a user-visible pain.
- **Spotify, Apple Music, YouTube Data API, Bandcamp, Discogs as sources.** All out per ADR-0007 alternatives table.
- **NSFW / explicit-content filtering.** v1 passes through provider responses; future spec if user reports demand it.
- **Saved searches.** Search history is persisted (last 50, show 10); "pin this search as a saved one" is a future spec.
- **User-OAuth flows on mobile** for SoundCloud or Last.fm. v1 uses server-side client-credentials (SC) / API-key (Last.fm) — no per-user redirect.
- **Mobile UX surface choice** (tab vs modal vs persistent search bar) — chosen during the spec's plan / by `ux-reviewer`, not gated by this spec. AC#20 is testID-driven and surface-agnostic.

## Design considerations

Patterns and trade-offs surfaced by the vault lookup (per `.claude/rules/vault-consultation.md`):

- [vault: wiki/concepts/Anti-Corruption Layer Pattern.md] — each external provider is wrapped in an ACL (Facade + Adapter + Translator); the domain sees `SearchResult`, never `DeezerTrackDTO` / `MBRecording` / `SCTrackJSON` / `LastFmTrackResp`. The vault's "Conformist" anti-pattern is what we explicitly avoid.
- [vault: wiki/concepts/Enterprise Integration Patterns.md] — **Scatter-Gather** ("broadcasts and consolidates responses") + **Aggregator** ("combines related messages into one") + **Normalizer** ("converts varying formats to a canonical form") are the named patterns. EIP caveat applied: no heavyweight ESB / message bus — straight `asyncio.TaskGroup`.
- [vault: wiki/concepts/Circuit Breaker Pattern.md] — per-source isolation; three states (Closed → Open → Half-Open); failure-threshold trips; **no retries v1** (vault: retry amplifies load on a struggling service).
- [vault: wiki/concepts/Bulkhead Pattern.md] — each provider has its own `httpx.AsyncClient` instance with an isolated connection pool; a slow Deezer cannot exhaust SoundCloud's resources.
- [vault: wiki/concepts/Bounded Context.md] — `discovery` is a new sibling to `catalog` / `library` / `playback` / `metadata`. `SearchResult` ≠ `Track`. ACL is the right Context Map relationship for the external-providers boundary.
- [vault: wiki/concepts/Hexagonal Architecture.md] — `SearchProvider`, `QueryCache`, `SearchHistoryRepository`, `SearchClickRepository` ports in `application/discovery/`; provider + cache + repository adapters in `adapters/outbound/discovery/`.
- [vault: wiki/concepts/API Design Principles.md] — **Tolerant Reader** pattern on both the client (mobile ignores unknown fields in `extras`) and the server (ACL ignores unknown provider fields).
- [vault: wiki/topics/API Design Overview.md] — REST + URI versioning + safe-idempotent GET; `POST /v1/discovery/clicks` is 202 Accepted because it's fire-and-forget; matches the project's prior REST stance from `view-library`.
- [vault: wiki/concepts/Idempotency.md] — `POST /v1/discovery/clicks` is server-side deduped within a 60s window on `(user_id, query_norm, result_signature)`; the client doesn't supply an idempotency key (per `view-library`'s precedent that GET-retry needs no key, this POST extends with server-side dedup because mobile may retry transparently on transient errors).

High-level approach (the *what + why*; the *how* lives in the plan):

- This is a **mixed-mode** path in the new `discovery` bounded context. The primary surface is **read** (scatter-gather + cache + dedup over external providers); two secondary surfaces are **append-only writes** (search-history per-search; click-tracking per-click) to internal Postgres tables.
- It **does** require a new bounded context (`discovery`) — first occupant — including new aggregates (`SearchHistoryEntry`, `SearchClick`), value objects (`SearchQuery`, `ResultKind`, `Confidence`, plus `Artist` / `Album` / `Playlist` moving from "Future" to "Canonical" in [docs/ubiquitous-language.md](../../ubiquitous-language.md)), ports (`SearchProvider`, `QueryCache`, `SearchHistoryRepository`, `SearchClickRepository`), use cases (`SearchMusic`, `LookupByUrl`, `RecordClick`, `ListSearchHistory`), four ACL adapters (deezer, musicbrainz, soundcloud, lastfm), one Redis adapter, and two SQLAlchemy repositories. Two Alembic migrations: one for `discovery_search_history`, one for `discovery_search_clicks`.
- It **does** introduce new external dependencies: `redis>=5.0` and `rapidfuzz` (runtime); `respx` confirmed as a dev dep. It **does** introduce new infrastructure: Redis (locked in [ADR-0007](../../adr/0007-unified-music-search.md)). Local dev gains a `redis:7-alpine` `docker-compose.yml` service. Prod gains a managed Redis host (Upstash free tier likely; capture during the plan's operational checklist).
- The full architectural decision record — source set, aggregation pattern, dedup model, caching tier, bounded-context name, telemetry posture, search-trigger choice, URL-paste posture, search-history v1, the v2 accuracy backlog — is locked in [ADR-0007](../../adr/0007-unified-music-search.md). This spec elaborates the user-facing acceptance criteria; it does not relitigate the ADR's decisions.

Wire contract for `GET /v1/discovery/search?q=&kinds=&limit=`:

```jsonc
200 OK
{
  "query": "the beatles",
  "query_norm": "beatles",
  "results": [
    {
      "kind": "track",                  // 'artist' | 'album' | 'track' | 'playlist'
      "title": "Let It Be",
      "subtitle": "The Beatles",        // tracks/albums: artist; artists: null; playlists: curator
      "image_url": "https://...",       // single canonical URL; null if no provider supplied one
      "confidence": "high",             // 'high' | 'medium' | 'low'
      "sources": [
        {"provider": "musicbrainz", "external_id": "...", "url": "https://musicbrainz.org/..."},
        {"provider": "deezer",      "external_id": "...", "url": "https://deezer.com/..."}
      ],
      "extras": {                       // per-kind enrichment; tolerant-reader
        "isrc": "GBAYE0601477",
        "duration_seconds": 243,
        "album": "Let It Be",
        "year": 1970,
        "preview_url": null             // reserved-null v1
      }
    }
  ],
  "providers": [
    {"provider": "musicbrainz", "status": "ok",       "result_count": 18, "latency_ms": 412},
    {"provider": "deezer",      "status": "ok",       "result_count": 23, "latency_ms": 178},
    {"provider": "lastfm",      "status": "ok",       "result_count": 14, "latency_ms": 305},
    {"provider": "soundcloud",  "status": "timeout",  "result_count": 0,  "latency_ms": 1500}
  ],
  "partial": true,
  "cache": {"hit": false, "fetched_at": "2026-05-27T15:23:11Z"}
}
```

Defaults + bounds: `kinds` defaults to all four; `limit` defaults to 25, bounded `1 ≤ limit ≤ 50` (lower max than `view-library`'s 200 because scatter-gather is more expensive per query); `q` is non-empty and ≤ 200 characters after trim. Out-of-range → HTTP 422 per AC#17.

Wire contract for `POST /v1/discovery/clicks`:

```jsonc
202 Accepted
// empty body
```

Wire contract for `GET /v1/discovery/search-history`:

```jsonc
200 OK
{ "items": [{"query": "the beatles", "query_norm": "beatles", "executed_at": "2026-05-27T15:23:11Z"}, ...], "total": 50 }
```

Schema (two Alembic migrations, both additive — no existing-table changes):

- `discovery_search_history` — `id UUID PK DEFAULT gen_random_uuid()`, `user_id UUID NOT NULL`, `query TEXT NOT NULL`, `query_norm TEXT NOT NULL`, `executed_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `result_clicked_signature TEXT NULL`. Index `discovery_search_history_user_idx` on `(user_id, executed_at DESC, id DESC)` for the trailing-N query.
- `discovery_search_clicks` — `id UUID PK DEFAULT gen_random_uuid()`, `user_id UUID NOT NULL`, `query_norm TEXT NOT NULL`, `result_signature TEXT NOT NULL`, `position INTEGER NOT NULL`, `confidence TEXT NOT NULL CHECK (confidence IN ('high','medium','low'))`, `clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()`. Unique constraint on `(user_id, query_norm, result_signature, clicked_at)` is **not** added; idempotency in the 60s window is enforced application-side (AC#16) to avoid uniqueness false-positives across the window boundary.

Query normalization rules (lives in `application/discovery/normalize.py`):

1. NFKC Unicode normalization
2. Lowercase (after NFKC)
3. Strip diacritics (`unicodedata.NFD` + drop combining marks)
4. Strip bracketed suffixes (`(Remastered 2009)`, `[Deluxe]`, `(feat. X)`)
5. Normalize feature notation (`feat.` / `ft.` / `featuring` / `with` → `feat`) before bracket-strip
6. Strip leading article on artist names (`the smiths` ↔ `smiths`, `los lobos` ↔ `lobos`; `the the` stays)
7. Collapse punctuation + whitespace (`&` → `and`; strip apostrophes/periods/commas; collapse spaces)
8. Trim

Confidence model (per ADR-0007):

- `ISRC-matched` (two providers report same ISRC) → `high`
- `JW ≥ 0.92` on normalized `(artist, title)` → `high`
- `JW ∈ [0.85, 0.92)` + winning per-source prior tie-breaker → `medium`
- Otherwise (provider's standalone result) → `low`

Per-source priors (tie-breaker only): MB 0.95, Deezer 0.85, Last.fm 0.80, SoundCloud 0.65.

Ranking within the response (multi-criteria sort, in order):
1. `confidence` DESC (high > medium > low) — three-level enum, not continuous score
2. **Multi-source boolean** DESC — `len(sources) > 1` evaluated as boolean True > False. A 4-source result and a 2-source result tie at this level (both True); a 1-source result ranks below. This is intentional: tier-2 is "did multiple providers agree?" not "how many."
3. Per-source-prior of the *winning* source DESC (the source whose title was chosen as canonical per the AC#3 representative-selection rule)
4. Alphabetical on `(subtitle, title)`

Not a Backend for Frontend [vault: wiki/concepts/Backend for Frontend Pattern.md]. Single REST API serves single mobile client; promotion to BFF stays deferred per [docs/specs/view-library/spec.md](../view-library/spec.md)'s precedent.

Mobile shape (parallel to `view-library`'s slice [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\apps\mobile\src\features\library\CLAUDE.md]):
- `apps/mobile/src/features/discover/` — first occupant of the discover slice
- Screen entrypoint: `ui/DiscoverScreen.tsx` (state machine on top of `_viewForState` pure helper; mirrors `LibraryScreen.tsx`'s pattern)
- Hooks: `hooks/useDiscoverSearch.ts` (React Query `useQuery` keyed on `query_norm`); `hooks/useSearchHistory.ts` (`useQuery` of the history endpoint)
- Client: `apps/mobile/src/shared/api-client/discovery.ts` — typed `searchDiscovery`, `lookupByUrl` (URL-paste path; same endpoint, mobile pre-detects URL and skips `kinds`), `recordClick`, `listSearchHistory`
- `testID`s per AC#20

## Dependencies

What this feature requires that must already exist or be built first:

- **Bounded contexts**: none (this spec creates `discovery`).
- **Other features**: `view-library` (shipped — gives the project a working catalog feature precedent); `auth-integration` (shipped via [ADR-0006](../../adr/0006-supabase-auth.md) — provides the verified `UserId` this spec consumes).
- **External services**:
  - Deezer public API (no auth)
  - MusicBrainz API (User-Agent registration + contact email required)
  - SoundCloud Developer API (app registration → `client_id` + `client_secret`, client-credentials OAuth flow)
  - Last.fm API (account registration → `LASTFM_API_KEY`)
  - Redis (Upstash free-tier likely for prod)
  - Postgres (already used by `view-library`)
- **Library/framework additions**:
  - Backend runtime: `redis>=5.0`, `rapidfuzz`. (`respx` confirmed already a dev dep per `services/api/CLAUDE.md`'s testing-setup section.)
  - Mobile: none beyond what `view-library` introduced (`@tanstack/react-query`, etc.).
- **Infrastructure**: `redis:7-alpine` added to `docker-compose.yml`; managed Redis provisioned for prod.
- **Documentation deliverables (must land in the same PR series as the feature, not after)**:
  - [docs/ubiquitous-language.md](../../ubiquitous-language.md) gains canonical entries for `SearchResult`, `SearchQuery`, `Confidence`, `ResultKind`, `SearchHistoryEntry`, `SearchClick` AND moves `Artist`, `Album`, `Playlist` from the "Future" section to "Canonical". The `terminology-drift` stop-hook will flag drift if missed.
  - [commitlint.config.js](../../../commitlint.config.js) scope-enum gains `discover-music-v1` (already done as part of `/feature-spec`'s side-effects).
  - This spec's status transitions from Draft → Clarify-gated → Ready-for-plan as the spec-reviewer subagent grants, then Planned → Shipped over the feature's lifecycle.

## Risks / open questions

- **Risk: SoundCloud OAuth client-credentials gotcha.** SoundCloud's developer-app registration was historically intermittently closed; if registration takes longer than expected to obtain credentials, slice work that depends on the SC adapter is blocked. Mitigation: the pre-spec checklist explicitly captures app registration as Step 1; the SC adapter can be slice-deferred if needed without affecting the other 3 providers in scatter-gather (the 4-source design tolerates one missing source).
- **Risk: MusicBrainz rate limiting cuts in.** Default rate is 1 req/s per IP; a registered User-Agent unlocks ~50 req/s (per [VERIFIED:WebSearch] "for user-agents associated with certain applications, MusicBrainz allows through (on average) 50 requests per second"). Mitigation: the `MUSICBRAINZ_USER_AGENT` setting carries the registered string + contact email; tested in adapter integration tests via header inspection.
- **Risk: scatter-gather budget too tight at p95.** 1500ms per-source / 2000ms total assumes parallel execution; if a single provider's tail-latency dominates, p95 of total latency degrades. Mitigation: telemetry from AC#5's `latency_ms` per-provider lets us tune the budget; v1 ships with 1500/2000 and we adjust in a no-spec config change if telemetry calls for it.
- **Risk: Redis outage blocks search.** Locked design says cache-miss-falls-through-to-live-provider, so Redis-down means uncached search (slower, rate-limit-pressured), not failed search. Mitigation: the `QueryCache` adapter's read-path treats Redis errors as cache-miss and logs `cache_unavailable`; verified by an integration test that kills the testcontainers Redis mid-test.
- **Risk: per-provider response shape changes silently break dedup.** ACL tolerant-reader stance (AC#19) drops the malformed result and continues, but a sustained schema change would silently degrade result quality. Mitigation: `provider_response_malformed` log events power an alertable metric ("provider X malformed > 1% of results for 10m"); ADR-0007 commits to revisiting if a provider changes shape.
- **Risk: search-history persistence is best-effort, not transactional with the search response.** If the history insert fails after the search response returned, the user sees a successful search but their history is missing the entry. Mitigation: history insert is awaited *before* the response is returned; if the insert fails, the search still returns 200 (the user's primary intent succeeded) and the failure is logged as `search_history_persist_failed`. This is a conscious choice — failing the search because the history failed would be worse UX.
- **Risk: telemetry logs raw normalized queries.** Privacy stance per ADR-0007: solo + small friends pool makes this acceptable v1; the ADR commits to revisiting at a growth milestone. Mitigation: queries are normalized (lowercase, diacritics stripped, etc.) which reduces personal-search-style leakage; structured-log format means redaction is grep-friendly when needed.
- **Open question: how does mobile know which provider URL pattern to detect for AC#10?** Resolved before plan: mobile sends `q` as-is (URL or text); the backend's URL-detection regex is the single source of truth. Mobile does not pre-filter or pre-strip.
- **Open question: should the empty-state's history rows truncate long queries?** Resolved: yes, truncate at 40 characters on the row with a `…` suffix; full query is preserved in storage and re-runs full-fidelity. This is UI detail, captured here so spec-reviewer doesn't flag it as missing.
- **Open question: does `image_url` need a fallback when no provider supplies one?** Resolved: `null` is the documented value; mobile shows a placeholder (existing design system token, no new asset). Captured in AC#20's design implicitly via the `discover-row-<id>` rendering.

## Telemetry

What we'd log / measure in production to know this works:

- **Log events** (structlog, JSON in prod per `platform/logging.py`):
  - `search_performed` — `user_id`, `query_norm` (raw normalized — per ADR-0007's privacy stance), `query_norm_hash` (SHA-256, 12-char prefix), `kinds`, `providers_called`, `result_count`, `total_latency_ms`, `partial`, `cache_hit`, `request_id`.
  - `search_provider_status` — `request_id`, `provider`, `status` (`ok`/`timeout`/`error`/`rate_limited`/`circuit_open`), `result_count`, `latency_ms`.
  - `search_result_clicked` — `user_id`, `query_norm_hash`, `result_signature`, `position`, `confidence`, `chosen_source`. Emitted by the `POST /v1/discovery/clicks` handler on successful persist.
  - `search_zero_results` — `query_norm`, `query_norm_hash`, `providers_called`. Worst-case query path; load-bearing for accuracy iteration.
  - `provider_response_malformed` — `provider`, `kind`, `missing_field`, `request_id`. Surfaces silent provider schema changes (AC#19).
  - `circuit_breaker_state_change` — `provider`, `old_state`, `new_state`, `failure_count_at_change`. Per [vault: wiki/concepts/Circuit Breaker Pattern.md] which calls out state changes as alertable.
  - `search_history_persist_failed` — `user_id`, `error_type`. Best-effort failure; doesn't fail the search response.
  - `cache_unavailable` — `error_type`. Redis-down indicator.
- **Metrics**: deferred — no metrics ADR yet. When that ADR ships, the minimum is:
  - request count + p50/p95/p99 latency for `GET /v1/discovery/search` and `POST /v1/discovery/clicks`
  - per-provider success rate + p95 latency
  - cache hit rate
  - circuit breaker state changes per provider per hour
- **Success metric for "did we beat legacy accuracy?"**: click-through rate on top-3 results, segmented by `confidence` bucket. If `high`-confidence top-3 CTR is significantly higher than `low`-confidence's CTR, the confidence model has signal. If equivalent, the model is noise → tune normalization rules or move to embeddings (per ADR-0007's v2 backlog).
- **Alerts**: none v1 (no on-call). When telemetry-ADR lands, alert candidates: `provider_response_malformed` > 1% sustained 10m; `circuit_breaker_state_change` to `open` for any provider; `cache_unavailable` for > 1m sustained.

## Related

- `[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]` — per-source ACL design
- `[vault: wiki/concepts/Enterprise Integration Patterns.md]` — Scatter-Gather + Aggregator + Normalizer (canonical names)
- `[vault: wiki/concepts/Circuit Breaker Pattern.md]` — per-source isolation, no-retry stance
- `[vault: wiki/concepts/Bulkhead Pattern.md]` — per-source connection-pool isolation
- `[vault: wiki/concepts/Bounded Context.md]` — `discovery` as a new sibling context
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — ports + adapters layout
- `[vault: wiki/concepts/API Design Principles.md]` — tolerant-reader on both sides; URI versioning
- `[vault: wiki/topics/API Design Overview.md]` — REST + safe-idempotent GET; POST-202 for fire-and-forget clicks
- `[vault: wiki/concepts/Idempotency.md]` — server-side 60s-window dedup on `/v1/discovery/clicks`
- ADR: [docs/adr/0007-unified-music-search.md](../../adr/0007-unified-music-search.md) — full decision record
- Brainstorm: [docs/brainstorms/2026-05-27-unified-music-search.md](../../brainstorms/2026-05-27-unified-music-search.md) — 13 decision streams + alternatives matrices
- Predecessor: [docs/specs/view-library/spec.md](../view-library/spec.md) — REST/pagination/state-machine patterns this spec inherits
- Predecessor: [docs/specs/auth-integration/spec.md](../auth-integration/spec.md) — the auth contract this spec consumes
