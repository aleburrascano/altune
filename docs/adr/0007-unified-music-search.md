# ADR-0007: Unified music search via scatter-gather across 4 providers + Redis adoption

- **Status:** Accepted
- **Date:** 2026-05-27
- **Deciders:** solo + Claude
- **Context tags:** [arch, tech-stack, pattern, layer, dependency]
- **Revisions:** 2026-05-27 — SoundCloud auth strategy switched from client-credentials OAuth to **yt-dlp `scsearch`** extraction after discovering that SoundCloud's Developer API now gates registration behind an Artist Pro subscription. The yt-dlp path is the same one the legacy `music-manager` shipped with [VERIFIED:Read@C:\Users\Alessandro\music-manager\backend\providers\soundcloud_provider.py#L21-L43]. Trade-offs in §"SoundCloud strategy revision" below. · 2026-05-28 — **Ranking overhaul**: relevance-first ranking (RRF + exact-match boost), confidence demoted to a display-only badge, **iTunes** added as a 5th provider, MusicBrainz ISRC revived (`inc=isrcs`), and a ranking-eval harness added. See §"Ranking-overhaul addendum" below.

## Context

Altune's next user-visible feature is a unified multi-source music search. Search accuracy was the legacy `music-manager`'s biggest pain — single-provider search returned wrong or missing canonical results, and the user has no confidence in a one-API approach. The pivot to this feature is documented in the brainstorm at `docs/brainstorms/2026-05-27-unified-music-search.md`.

This decision locks: the **source set**, the **aggregation pattern**, the **confidence/dedup model**, the **caching infrastructure** (which is large enough to be its own decision; bundled here per the ADR-0006 precedent of folding a closely-related dependency into the same ADR), the **new bounded context name**, the **resiliency stance**, and the **telemetry posture**. Eight downstream tactical questions (search trigger, URL-paste, search history, ranking, query normalization, etc.) are also locked here so the feature spec doesn't relitigate them.

Auth (ADR-0006) provides the verified `UserId` the search endpoint authenticates against [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\docs\adr\0006-supabase-auth.md#L42-L42]. Persistence (ADR-0003) provides the Postgres the search-history + click-tracking tables live on. This ADR builds on both.

## Decision

Implement a new `discovery` bounded context whose `SearchMusic` use case fans out scatter-gather across **four** external providers — **Deezer**, **MusicBrainz**, **SoundCloud**, and **Last.fm** — via `asyncio.TaskGroup`, with per-source 1500ms timeouts and a 2000ms overall budget. Each provider is an outbound adapter behind a `SearchProvider` port (hexagonal); each adapter is the provider's **Anti-Corruption Layer**, translating provider DTOs into altune's domain `SearchResult` so the domain never sees provider terms. Results are merged into a single ranked list using an **ISRC-when-present + Jaro-Winkler ≥ 0.92 + per-source-priors** confidence/dedup model, with confidence emitted to mobile as `{high, medium, low}`. Each provider has its own **circuit breaker** (5 consecutive failures → open 30s) and its own `httpx.AsyncClient` for **bulkhead** isolation; no retries v1. Cache results in **Redis** (newly adopted infrastructure dependency), keyed `discovery:v1:<source>:<kinds_sorted_csv>:<sha256_of_query_norm>`, with per-source TTL (MB 24h / Last.fm 12h / Deezer 6h / SC 1h), cached as the post-ACL canonical shape (not raw provider JSON). Search trigger is **submit-only v1** with an explicit **as-you-type fast-follow** commitment for v1.1. **URL-paste resolution** and **per-user search history** ship v1. **Telemetry logs raw normalized queries** alongside their hash; ADR commits to revisiting this when the user base grows beyond the friends pool.

## Alternatives considered

| Alternative | Why not |
|---|---|
| **Single-source (Deezer-only) v1** | Throws away multi-source confidence signal — exactly the legacy pain. Defeats the feature's stated motivation. |
| **Spotify as a source** | User constraint (paywall). Worth a future revisit if search-only access is verified free. |
| **Apple Music as a source** | $99/yr Apple Developer Program membership [VERIFIED:WebSearch] "Using the Apple Music API requires an Apple Developer account which costs $99 per year"; same paywall posture as Spotify. |
| **YouTube Data API as a source** | 10k quota units/day × 100 units/search = ~100 searches/day default [VERIFIED:WebSearch] "Search requests cost 100 units, which means with the default 10,000 daily units, you could perform approximately 100 search requests per day". Useless as primary provider. |
| **Bandcamp as a source** | Public API limited to oEmbed [VERIFIED:WebSearch] "the platform does not extend to offering a dedicated API for developers to access metadata programmatically beyond the public oEmbed and embedding features"; full search requires partner agreement. |
| **Discogs as a source** | Heavy MusicBrainz overlap; auth setup + 25–60 req/min rate limit doesn't pay back for v1. |
| **Sequential with early-return** instead of scatter-gather | Each provider adds tail latency; can't combine cross-source signal for confidence. |
| **Per-result-type routing** (e.g., MB for albums, Deezer for tracks) | Throws away multi-source signal; brittle when sources gain/lose result-type coverage. |
| **String similarity only** (no ISRC) for dedup | Misses ISRC-canonical match opportunity — the strongest dedup signal Deezer + MB both expose. |
| **Embedding-based confidence** | Premature ML infrastructure; solo project; JW + ISRC + priors is sufficient for v1 and revisitable. |
| **Postgres cache table** instead of Redis | Uses existing infra (ADR-0003) but slower TTL semantics; user explicitly opted for Redis to lock in a fast cache primitive available to future features. |
| **In-process LRU cache** | No persistence; dies on restart; no per-source TTL. |
| **No cache v1** | Deezer's 50-req/5s budget [VERIFIED:WebSearch] "The rate limit is 50 requests per 5 seconds" + MB's 1-req/s default would burn instantly. |
| **`search` or `music_search` as the bounded context** | `search` collides with future in-library search; `music_search` is verbose vs the existing single-word context names (`catalog`, `library`, `playback`, `metadata`). |
| **Search-as-you-type v1** | Requires AbortController + stale-response guards + 10× cache pressure. Locked as v1.1 fast-follow instead. |
| **Defer URL-paste resolution to v1.1** | Small scope; high UX value for the SoundCloud leak/share flow the user explicitly prioritized. Ship v1. |
| **Defer per-user search history to v1.1** | Cheap to land now (one new table; ring-buffer of 50, empty-state shows 10); locks the persistence boundary while it's fresh. |
| **Hash-only telemetry** | Privacy-preserving but tunes-blind. User chose content+hash to enable accuracy iteration; ADR commits to revisiting at growth milestone. |
| **Cursor pagination v1** | Multi-source offset state encoding + signing is significant scope; top-N (25/source → ~30–50 merged) suffices v1. |
| **`python-Levenshtein` for similarity** | `rapidfuzz` is faster, more maintained, has equivalent API. Adapter-internal choice. |
| **Cache raw provider JSON** instead of post-ACL `SearchResult` shape | ACL evolution would invalidate the cache silently (clients see stale-shape results). Caching the canonical domain shape means version-prefix bump = clean invalidation. |
| **One ADR per topic** (split discovery + Redis into two) | Redis is being adopted *for* discovery; reads cleaner as one decision. Precedent: ADR-0006 folded mobile auth client into the same ADR as the verification mode. |

## Consequences

### What becomes easier

- **Search accuracy is measurable.** The telemetry posture (raw `query_norm` + hash, top-3 CTR by confidence bucket) gives a concrete signal for whether the ISRC+JW model beat the legacy single-API approach. Without this, the legacy pain would just repeat invisibly.
- **Future features get fast cache without re-deciding.** Redis is now available for session blocklists, rate-limit token buckets, ephemeral per-user state, etc. No more "should we add Redis?" ADR ever.
- **Adding/removing providers is contained.** Each provider is one adapter behind one port; the `sources: []` array on the response is open-closed; no domain change required.
- **Downstream specs have a stable contract.** `play-from-search`, `add-result-to-library`, and `subscribe-to-artist` all consume the locked response shape — they don't need to refactor it.
- **The leak/unreleased surface is first-class day-1.** SoundCloud is in the scatter-gather from slice 1; the `confidence` bucketing + per-source prior model ensures fan-uploads surface as separate results when ISRC is absent, exactly as the user intent requires.
- **URL-paste flow turns share-links into first-class results.** Tapping a SoundCloud share link from outside the app and pasting it into search resolves directly to the canonical result.

### What becomes harder

- **Four operational dependencies before Slice 1.** SoundCloud developer-app registration (`client_id` + `client_secret`), Last.fm API-key registration (`LASTFM_API_KEY`), MusicBrainz registered User-Agent string + contact email [VERIFIED:WebSearch] "Each request sent to MusicBrainz needs to include a User-Agent header with enough information for MusicBrainz to contact the application maintainers", and Redis provisioning (Upstash free-tier likely; 10k commands/day). Each is a real account/credential to obtain.
- **New piece of infrastructure to keep current.** Redis security patches, version bumps, prod host selection, env wiring. Lifetime cost beyond v1.
- **Local dev now requires Redis.** `redis:7-alpine` in `docker-compose.yml`; `uv run` workflow gains a dependency.
- **Four providers means four ACLs to write, version, and test.** Each adapter has its own `respx` fixtures, its own translation logic, its own per-source quirks (MB's User-Agent demand, SC's OAuth client-credentials, Last.fm's API-key-in-querystring, Deezer's no-auth).
- **Privacy commitment.** Logging raw `query_norm` is documented as a v1-acceptable choice (solo + small friends pool); the ADR commits to revisiting + adding an explicit disclosure when user base grows.
- **Scatter-gather budget is tight.** 1500ms per-source under a 2000ms overall budget is realistic with circuit breakers + parallelism but requires careful adapter implementation (no synchronous fallbacks; correct `httpx` timeout wiring; correct propagation of cancellation through `TaskGroup`).

### What we're committing to (and the cost to reverse)

- **Redis as an altune infrastructure primitive.** Reversal = re-add a cache adapter that hits Postgres or in-process LRU, plus a runbook for users who depended on cross-restart cache warmth. Moderate cost; the `QueryCache` port abstracts the implementation, so the change is contained to one adapter.
- **`discovery` as the bounded-context name + `discover-music-v1` as the spec name.** Renaming a bounded context after multiple features ride on it is a sweeping refactor (folders, imports, glossary, scope-enum, route prefixes, mobile feature folder, terminology drift). Cheap to reverse if done before slice 1; expensive after.
- **The `GET /v1/discovery/search` response shape.** Three+ downstream specs are designed around it. Breaking changes require either versioning (`/v2/discovery/...`) or tolerant-reader migrations. The shape is intentionally future-proof (`sources: []`, `extras: {}`, reserved `preview_url: null`) so additive evolution is cheap.
- **Scatter-gather as the aggregation pattern.** Reversal to sequential would change p95 latency profile + lose multi-source confidence signal. Unlikely to want to reverse.
- **ISRC + JW + per-source-priors as the dedup contract.** Reversible to embeddings or other models adapter-internally (the use case sees `SearchResult.confidence` only). Cheap.
- **Raw `query_norm` in logs.** Reversal = log redaction pass + retroactive log pruning, plus an updated privacy stance. Cheap if done before scale; nontrivial after.

## SoundCloud strategy revision (2026-05-27)

The original decision specified SoundCloud's Developer API + client-credentials OAuth. On account-registration attempt, the developer portal required an **Artist Pro** subscription (a paid producer/musician tier), which is not available to solo non-artist developers. Without Artist Pro, the official API path is blocked entirely — not just rate-limited.

Resolution: switch to **yt-dlp's `scsearch:` extraction** — the same approach the legacy `music-manager` shipped with [VERIFIED:Read@C:\Users\Alessandro\music-manager\backend\providers\soundcloud_provider.py#L66-L68] (`ydl.extract_info(f"scsearch{limit}:{query}", download=False)`). yt-dlp handles SoundCloud's public web surface natively; no API key, no OAuth.

Consequences of the revision:

- **No SoundCloud env vars.** `SOUNDCLOUD_CLIENT_ID` / `SOUNDCLOUD_CLIENT_SECRET` removed from the env contract. One fewer operational dependency.
- **New runtime dep**: `yt-dlp`. Used for `SearchProvider.search()` on SoundCloud only; no download/streaming.
- **Result-type narrowing**: yt-dlp's `scsearch:` returns **tracks only**. SoundCloud's per-kind coverage in v1 contracts from `{tracks, sets/playlists, user pages/artists, partial albums}` to **tracks only**. Result-type matrix updated. Playlists in v1 are now Deezer-only; future SoundCloud spec can add `scset:` / `scuser:` if/when Artist Pro becomes feasible or yt-dlp adds those extractors.
- **Async wrapping**: yt-dlp is synchronous. The adapter must run extraction in `asyncio.to_thread` (per `services/api/CLAUDE.md`'s "CPU-bound work in `asyncio.to_thread`" rule) so the scatter-gather TaskGroup's event loop isn't blocked.
- **Bulkhead concern**: the per-source `httpx.AsyncClient` bulkhead does NOT protect against a slow yt-dlp call — `asyncio.to_thread` uses a shared thread pool. Mitigation: the per-source `asyncio.wait_for(1500ms)` timeout still bounds the worst case, and the per-source circuit breaker (5 fails / 30s) trips yt-dlp out of the rotation under sustained failure.
- **ToS posture**: yt-dlp extraction operates against SoundCloud's public web pages. Legal/ToS posture is the same as the legacy `music-manager` (which shipped without incident); documented as a known risk in this ADR. If/when an Artist Pro path becomes feasible, swap is one new ACL adapter; the use case sees no change.
- **Result confidence**: the per-source prior for SoundCloud stays at **0.65** (uploader-as-artist is fuzzy regardless of access path). yt-dlp output lacks ISRC, so dedup via ISRC is impossible for SC; JW similarity on `(uploader, title)` is the only dedup signal — matches the original brainstorm's stance.

The bullets below in "Decision" + the alternatives table reflect the revised strategy.

## Ranking-overhaul addendum (2026-05-28)

Shipped discovery search felt broken: the obviously-correct match for a query surfaced *mid-list*, not at the top. Root cause: the original ranking sort was `{confidence DESC → multi-source DESC → prior DESC → alpha}` — **no term measured relevance to the query**, and it discarded each provider's own relevance ordering. Confidence (a dedup-agreement signal) was conflated with relevance and used as the primary sort key, so agreed-upon-but-less-relevant entries outranked the canonical hit. Separately, the ISRC dedup signal was effectively dead: MusicBrainz omitted ISRC, leaving Deezer as the only ISRC-bearing source with no partner to match.

This addendum revises three ADR-0007-locked decisions:

1. **Ranking model → relevance-first.** Replace the confidence-primary sort with a **continuous query-relevance score** as the primary key, with cross-provider agreement (RRF) as a secondary corroboration tiebreak and a **relevance floor** that excludes non-matches.
   - *Relevance* `rel = max(token_set_ratio(query, title), token_set_ratio(query, "artist title")) / 100` (rapidfuzz, graded 0–1). Banded to 0.1 for the sort so near-equal relevance lets agreement decide.
   - *RRF* `Σ 1/(60 + best_rank)` over **distinct providers** (each provider once, at its best rank — same-provider duplicates can't inflate score or confidence).
   - *Floor* `REL_FLOOR` (initial 0.50, eval-calibrated): results below it are dropped, so an unsatisfiable query returns real matches or zero-results, not provider-rank noise.
   - Sort key: relevance-band DESC → RRF DESC → distinct-multi-source DESC → prior DESC → alpha.
   - **Confidence becomes display-only** (trust badge; a same-provider-only merge stays LOW since there's no independent agreement). Implemented as `fuse_and_rank(per_provider, query_norm)` in `application/discovery/dedup.py`; the use case passes per-provider groups. Merge semantics (ISRC/JW collapse, canonical representative) unchanged.
   - **Why not RRF-primary + binary boost** (the first cut, rejected): RRF is rank *fusion*, not relevance *measurement* — it trusts each provider's position, so for queries a provider can't satisfy it promotes that provider's fuzzy fallbacks. A binary "all query tokens present" boost gave partial matches (the common case) zero credit, leaving RRF + prior to rank noise (observed: "Under Pressure" ranked #1 for `che rest in bass`). The first eval set hid this by testing only clean artist+title queries (false MRR 1.00).

2. **Source set → add iTunes (5th provider).** The **iTunes Search API** (`https://itunes.apple.com/search`) is free and requires no auth — a *different* API from the paid Apple Music API rejected above (the "Apple Music as a source" row referred to the $99/yr developer-program API). iTunes carries no ISRC but adds strong relevance, artwork (100×100 upscaled to 600×600), and preview URLs (filling the reserved `extras.preview_url`). Per-source prior 0.85 (commercial catalog, like Deezer); cache TTL 12h. ~20 calls/min, covered by the per-source cache. SoundCloud is retained for now; pruning is a future, data-driven call once the eval harness shows each provider's contribution.

3. **Revive ISRC.** MusicBrainz recording search now requests `inc=isrcs`; the first ISRC populates `extras["isrc"]`, restoring the canonical high-precision cross-source merge with Deezer/iTunes.

**Measurement.** A deterministic ranking-eval harness lands at `services/api/tests/eval/` (golden query set → MRR + top-3 hit-rate, regression-guarded in CI) plus `scripts/ranking_eval.py` for live spot-checks. The golden set is **expanded beyond clean "artist + title" queries** to include partial (title-only / artist-only), misspelled, ambiguous (many same-title artists), and unsatisfiable/nonsense queries, with a **no-junk invariant** (every returned result for an unsatisfiable query clears `REL_FLOOR`). This is the success metric ADR-0007 §Consequences called for ("search accuracy is measurable") — and the expanded set exists specifically because the first happy-path-only set reported a false MRR 1.00 while live `che rest in bass` still surfaced an irrelevant #1. Before/after numbers on the expanded set are recorded with the implementation.

**Reversibility.** All three are adapter/application-internal — the `GET /v1/discovery/search` response shape is unchanged (still `sources: []` + `confidence` + `extras: {}`), so mobile and downstream specs are unaffected. RRF constants and boost magnitudes are tunable; iTunes is one adapter behind the existing `SearchProvider` port.

## Strangler-rebuild addendum (2026-06-21)

The Go discovery pipeline (the port of the Python service above) had accumulated ~13 sequential transforms and a scatter of tuned constants across many sessions. Plan 003 rebuilt it as a parallel `internal/discovery2` package via a **strangler-fig**: built layer-by-layer behind the existing handler, gated at every step by a library-derived **top-K eval**, with the old package retained for instant rollback. Cutover is a per-surface config flag (`DISCOVERY_V2_SEARCH`, default off).

**The load-bearing learning — "categorical" is not automatically principled.** The rebuild's first attempt tried to remove tuned constants by making decisions *categorical*: a version-marker **keyword vocabulary** (remix/live/deluxe/remaster/…) for merge, and **lexicographic relevance tiers** for rank. The full-catalog eval **regressed** (top-3 97.9% vs v1 98.9%). Root cause: a ~25-keyword list is **a tuned constant in disguise** — it replaced one magic number with many magic strings, and it over-merged release variants (folding `Big Poppa - 2005 Remaster` into `Big Poppa (2007 Remaster)`, whose parens normalization strips). The tiers were **pattern-fit and brittle** — they leaned on vocabulary-based intent detection that frequently fails, then collapsed to popularity.

**Decision — principled signals only, converging back on this ADR's 2026-05-28 relevance-first model:**

1. **Merge (Layer 2) → identifiers + canonical equality.** Same ISRC/MBID (exact) → same entity; otherwise **exact `textnorm`-canonical title + artist** equality. The shared canonical normalization *is* the "same title" decision: a trailing sequel number survives it (`Shotta Flow 2` ≠ `Shotta Flow`, so the numbered-sequel collapse — "Pattern B" — is fixed *for free*), while a parenthetical `(Remaster)`/`(Live)` is canonical noise and folds away. **No keyword list, no version regex, no fuzzy threshold.**
2. **Rank (Layer 3) → continuous relevance.** `rel = max(token_sort_ratio(q, title), token_sort_ratio(q, "artist title")) / 100` (rapidfuzz) → popularity → distinct-multi-source → RRF (`k=60`). **No bands, no tiers, no intent contract.** This is the same relevance-first instinct as the 2026-05-28 addendum; the Go rebuild had briefly abandoned it for tiers and measurably regressed.
3. **Also removed as query-fit:** vocabulary-based intent detection (providers now do plain search), the "did you mean" suggestion (tuned relevance threshold), the consensus MusicBrainz "authority filter" + "zero-overlap discard" thresholds (kept only an operational per-request lookup cap), and the vocab-learning popularity threshold.
4. **No kind tiebreak.** The product bar is **top-3** ("the right answer must be *visible*, not strictly #1"), which makes "same-named album outranks the track" (Pattern A) moot — the track sits at #2/#3 and passes. `track>album>artist` remains a *non*-query-fit lever held in reserve for top-1 polish only.

**Measurement (full-catalog head-to-head, 1,792 entities, identical run, 2026-06-21):** v2 **99.0% top-3 / 18 failures** vs v1 **98.9% / 20** — the query-fit-free pipeline **meets and beats** the tuned one on the gate, with net +2 driven by real structural wins (the numbered sequels `Shotta Flow 2`/`3` that v1 collapses, plus correct remaster-variant resolution). Top-1 traded down (93.6% vs 96.9%) — the accepted top-3-moot Pattern-A effect. The `discoveryeval --pipeline v1|v2` harness exercises either pipeline through one `Searcher` seam.

**Reversibility.** Adapter/application-internal; the `GET /v1/discovery/search` response shape is unchanged. The old `internal/discovery` package is retained; cutover and rollback are the `DISCOVERY_V2_SEARCH` flag. ML-learned scorers remain a future, separate seam (plan 004).

## Implementation notes

Pre-spec operational checklist (must be done before Slice 1 of the feature spec):

1. ~~Register the altune SoundCloud developer app~~ — superseded; SoundCloud via yt-dlp requires no registration (revision 2026-05-27).
2. Register a Last.fm API account → capture `LASTFM_API_KEY` (and reserve `LASTFM_SHARED_SECRET` for future write endpoints).
3. Choose the prod Redis host (Upstash free-tier likely; the 10k commands/day cap is comfortably above solo + small friends pool) → capture `REDIS_URL` for prod env.
4. Confirm MusicBrainz registered User-Agent string format + contact email.
5. Capture frozen-fixture JSON responses from each of the 4 providers for `respx` mocking in CI — one happy-path per (provider, kind) — so integration tests don't make live calls.

New runtime dependencies the spec will add via `uv add`:
- `redis>=5.0` (native asyncio via `redis.asyncio.Redis`)
- `rapidfuzz` (similarity library; adapter-internal)
- `yt-dlp` (SoundCloud search via `scsearch:` extraction — see strategy revision)
- `respx` confirmed as a dev dependency for adapter integration tests.

Settings additions (`platform/config.py`):
- `redis_url: RedisDsn`
- `lastfm_api_key: SecretStr`
- `musicbrainz_user_agent: str` (validated to contain a contact form per MB's requirement)
- ~~`soundcloud_client_id` / `soundcloud_client_secret`~~ — removed per the strategy revision

`docker-compose.yml`: add `redis:7-alpine` service with a healthcheck.

Mobile UX surface (tab vs modal vs persistent search bar): deferred to the spec; `ux-reviewer` subagent gates. Backend-agnostic.

Spec name: `discover-music-v1` (17 chars; under the ≤25-char limit per `/feature-spec`). The skill auto-appends to commitlint's `scope-enum` [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\commitlint.config.js#L35-L38] and adds the new domain terms (`SearchResult`, `SearchQuery`, `Confidence`, `ResultKind`, `SearchHistoryEntry`) to [docs/ubiquitous-language.md](../ubiquitous-language.md), moving `Artist` / `Album` / `Playlist` from the Future section to Canonical.

Bounded-context layout this commits to:
```
services/api/src/altune/
├── domain/discovery/                     # SearchResult, SearchQuery, Confidence, ResultKind,
│                                         # Artist, Album, Playlist, SearchHistoryEntry, events
├── application/discovery/                # SearchProvider, QueryCache ports; SearchMusic use case;
│                                         # normalize_for_match()
└── adapters/
    ├── inbound/http/discovery/           # GET /v1/discovery/search; POST /v1/discovery/clicks
    └── outbound/discovery/
        ├── deezer/                       # ACL adapter (no auth)
        ├── musicbrainz/                  # ACL adapter (User-Agent required)
        ├── soundcloud/                   # ACL adapter (yt-dlp scsearch via asyncio.to_thread)
        ├── lastfm/                       # ACL adapter (API key in querystring)
        └── cache/redis_cache.py          # QueryCache implementation
```

Mobile feature slice: `apps/mobile/src/features/discover/` — first occupant of the discover surface, following the `library/` pattern [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\apps\mobile\src\features\library\CLAUDE.md].

## Vault references

- `[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]` — per-source ACL: Facade + Adapter + Translator; domain never sees Deezer/MB/SC/Last.fm terms
- `[vault: wiki/concepts/Enterprise Integration Patterns.md]` — Scatter-Gather (canonical name), Aggregator, Normalizer; EIP caveat applied (no heavyweight ESB — straight `asyncio.TaskGroup`)
- `[vault: wiki/concepts/Circuit Breaker Pattern.md]` — per-source isolation; three states (Closed → Open → Half-Open); no retries v1 per the vault's "retry amplifies load" guidance
- `[vault: wiki/concepts/Bulkhead Pattern.md]` — per-source `httpx.AsyncClient` connection-pool isolation prevents one slow provider from exhausting another's resources
- `[vault: wiki/concepts/Bounded Context.md]` — `discovery` as a new sibling to `catalog` / `library` / `playback` / `metadata`; ACL is the right Context Map relationship for external providers
- `[vault: wiki/concepts/Hexagonal Architecture.md]` — `SearchProvider` + `QueryCache` ports in `application/discovery/`; provider + cache adapters in `adapters/outbound/discovery/`
- `[vault: wiki/concepts/Idempotency.md]` — `GET /v1/discovery/search` is safe + idempotent (no idempotency key); `POST /v1/discovery/clicks` deduped server-side within a 60s window

## Related

- **Brainstorm:** [docs/brainstorms/2026-05-27-unified-music-search.md](../brainstorms/2026-05-27-unified-music-search.md) — full decision matrix + alternatives analysis + 13 decision streams
- **Predecessor:** [ADR-0003](0003-persistence-stack.md) — persistence stack this builds on (new tables `discovery_search_history` + `discovery_search_clicks`)
- **Predecessor:** [ADR-0005](0005-mobile-server-state-react-query.md) — React Query is what `apps/mobile/src/features/discover/` uses for search-result server state
- **Predecessor:** [ADR-0006](0006-supabase-auth.md) — `current_user_id` is the input to per-user search-history persistence and click-tracking
- **Spec (next):** `docs/specs/discover-music-v1/spec.md` (to be created by `/feature-spec discover-music-v1`)
