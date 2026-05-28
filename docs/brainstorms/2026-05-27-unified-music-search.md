---
date: 2026-05-27
status: locked
graduates-to: docs/adr/0007-unified-music-search.md (next), docs/specs/discover-music-v1/spec.md (after)
related:
  - docs/adr/0003-persistence-stack.md
  - docs/adr/0005-mobile-server-state-react-query.md
  - docs/adr/0006-supabase-auth.md
  - docs/architecture.md
  - docs/ubiquitous-language.md
  - "[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]"
  - "[vault: wiki/concepts/Enterprise Integration Patterns.md]"
  - "[vault: wiki/concepts/Circuit Breaker Pattern.md]"
  - "[vault: wiki/concepts/Bounded Context.md]"
  - "[vault: wiki/concepts/Hexagonal Architecture.md]"
---

# Brainstorm — Unified music search (`discover-music-v1`)

## 1. Frame

The next altune feature is a unified multi-source music search: one query → ranked results across **artists, albums, singles, and playlists** from multiple external APIs, regardless of which source has the canonical version. Pivoted from a paused `import-tracks-v1` (handoff in [velvet-tumbling-squirrel.md](../../C:/Users/Alessandro/.claude/plans/velvet-tumbling-squirrel.md), blocked on legacy-schema discovery + playback).

Fixed user constraints:
- Spotify out (paywall posture).
- SoundCloud will later host unreleased/leak tracks; those must feel first-class, not a separate surface.
- No download/streaming yet — **search only**.
- Search accuracy was the legacy `music-manager`'s biggest pain — confidence/dedup is the load-bearing decision.

Project stack constraints (verified against current code):
- Backend: Python 3.12+, FastAPI, async-first, hexagonal layers ([services/api/CLAUDE.md](../../services/api/CLAUDE.md)).
- `domain/` is pure Python — no httpx, no Pydantic ([.claude/rules/domain-layer.md](../../.claude/rules/domain-layer.md)).
- Auth landed via ADR-0006: every authenticated endpoint receives a verified `UserId` from `current_user_id` ([docs/adr/0006-supabase-auth.md](../adr/0006-supabase-auth.md)).
- Mobile: vertical slice in `apps/mobile/src/features/<feat>/`, React Query for server state.

**What this brainstorm decides** (13 decision streams + Claude-call details):
1. Source set
2. Aggregation strategy
3. Confidence / dedup model
4. Caching
5. Bounded context name
6. Query normalization rules
7. Response shape (wire contract)
8. Pagination
9. Search trigger (UX)
10. URL-paste resolution
11. Per-user search history
12. Telemetry posture
13. Result ranking, domain events, accuracy v2 backlog

**What this brainstorm does NOT decide:**
- Mobile UX surface (tab vs modal vs persistent bar) — `ux-reviewer` gates in the spec.
- Track-playback / preview — `track-playback` future spec; v1 reserves `preview_url: null` in the wire shape.
- Library-write surface (save-from-search) — future spec on top of the locked sources[] contract.
- File-fingerprint search (AcoustID-style) — different surface; not v1.

## 2. Vault patterns leaned on

- **[vault: wiki/concepts/Anti-Corruption Layer Pattern.md]** — each external API gets its own ACL (Facade + Adapter + Translator); domain never sees provider terms.
- **[vault: wiki/concepts/Enterprise Integration Patterns.md]** — Scatter-Gather (canonical name for "broadcasts and consolidates responses"), Aggregator, Normalizer. EIP caveat: apply selectively, no heavyweight ESB — straight `asyncio.TaskGroup`.
- **[vault: wiki/concepts/Circuit Breaker Pattern.md]** — per-source failure isolation; N sources hit on every query is exactly the named case. No retries v1 (vault: retry amplifies load).
- **[vault: wiki/concepts/Bounded Context.md]** — `discovery` is a new sibling to catalog/library/playback/metadata. `SearchResult` ≠ `Track`.
- **[vault: wiki/concepts/Hexagonal Architecture.md]** — `SearchProvider` port in `application/discovery/`, provider adapters in `adapters/outbound/discovery/<source>/`.

## 3. Decision streams

### 3.1 Source set

Candidates considered + verdict:

| Source | Verdict | Reason |
|---|---|---|
| **Deezer** | **In v1** | Free public API, no auth for search, 50 req/5s, full result-type breadth (artists, albums, tracks, playlists). |
| **MusicBrainz** | **In v1** | Free, canonical metadata + ISRC + MBID for disambiguation. User-Agent required, 1 req/s default. |
| **SoundCloud** | **In v1, via yt-dlp** | None (yt-dlp `scsearch:` extraction). The leak/unreleased surface. Tracks only. |
| **Last.fm** | **In v1** | Free with API key. Complementary metadata (artist tags, similar artists, listener counts). |
| Discogs | Deferred | Heavy MB overlap; auth setup + 25–60 req/min doesn't pay back. |
| Spotify | Out | Paywall constraint (per user; verify if search-only-free changes the calculus in a future revisit). |
| Apple Music | Out | $99/yr Apple Developer Program. |
| YouTube Data API | Out | 10k quota/day × 100 units/search = ~100 searches/day. Useless as a primary provider. |
| YouTube Music | Out | No public API. |
| Tidal | Out | Gated access. |
| Bandcamp | Out | Public API limited to oEmbed; full search needs partner agreement. |
| Mixcloud / Audius / Jamendo | Out | Catalogs too narrow for a v1 scatter-gather leg. |
| AcoustID, Setlist.fm, Genius, AudioDB, AllMusic, Pandora | Out | Different surface (fingerprint / setlists / lyrics) or no public API. |

**Locked:** Deezer + MusicBrainz + SoundCloud + Last.fm.

**SoundCloud strategy revision (2026-05-27):** the original brainstorm specified client-credentials OAuth via the SoundCloud Developer API. On registration attempt the dev portal required an **Artist Pro** subscription, blocking solo-developer access entirely. Switched to **yt-dlp's `scsearch:` extraction** — the same path the legacy `music-manager` shipped with [VERIFIED:Read@C:\Users\Alessandro\music-manager\backend\providers\soundcloud_provider.py#L66-L68]. Consequences: no SoundCloud env vars; new runtime dep `yt-dlp`; result-type narrowed to **tracks only** (no `scset:` / `scuser:` extractors in scope v1; playlists become Deezer-only); adapter runs in `asyncio.to_thread` because yt-dlp is sync. Per-source prior stays 0.65; no ISRC available so JW is the only dedup signal for SC.

**Per-source priors (used in the confidence tie-breaker, see §3.3):**
- MusicBrainz 0.95 — MBID-canonical
- Last.fm 0.80 — curated tags + scrobble signal
- Deezer 0.85 — catalog-quality but pop-centric
- SoundCloud 0.65 — user-uploaded, ambiguous (intentional — leak uploads *should* surface separately when ISRC absent)

### 3.2 Aggregation strategy

**Locked: Scatter-Gather with `asyncio.TaskGroup`** ([vault: wiki/concepts/Enterprise Integration Patterns.md] — pattern is named for this case).

| Setting | Value |
|---|---|
| Fan-out primitive | `asyncio.TaskGroup` (mandated by [.claude/rules/python-backend.md](../../.claude/rules/python-backend.md)) |
| Per-source timeout | 1500 ms wall-clock (`asyncio.wait_for`) |
| Overall budget | 2000 ms (caller-side timeout in the use case) |
| Partial results | Allowed; merge what arrived; emit `partial: true` + per-source status in response |
| Retries v1 | **None** (vault Circuit Breaker note: retry amplifies failure) |
| Circuit breaker per source | 5 consecutive failures → open for 30s; while open, source returns immediately with `status: "circuit_open"` |
| Bulkhead | Each provider gets its own `httpx.AsyncClient` instance + connection pool |

### 3.3 Confidence / dedup model

**Locked: ISRC-when-present + Jaro-Winkler ≥ 0.92 + per-source priors as tie-breaker.**

1. **Canonical key** when present: ISRC (12-char ISO 3901). Two results sharing an ISRC → collapse into one merged `SearchResult` with `sources: [...]`.
2. **Soft match**: normalize both sides (see §3.6), then JW similarity on `f"{artist}|{title}"`. JW ≥ 0.92 = same.
3. **Per-source priors** as tie-breaker when JW ∈ [0.85, 0.92): see §3.1 priors.
4. **Confidence levels** emitted as `{high, medium, low}` derived from `{ISRC-matched, JW≥0.92, JW∈[0.85,0.92)}`.
5. **Library:** `rapidfuzz` (adapter-internal; faster + more maintained than `python-Levenshtein`).
6. **No embeddings v1.** Defer until JW telemetry shows specific failure modes.

Named trade-off: SoundCloud user-upload dedup will be weaker than Deezer/MB dedup because user-uploads lack ISRC. Acceptable v1 — those *are* the "different upload of the same canonical track" the user wants to see surfaced, not collapsed.

### 3.4 Caching

**Locked: Redis from day-1 (new infrastructure, ADR-level).**

Cache key:
```
discovery:v1:<source>:<kinds_sorted_csv>:<sha256_of_query_norm>
```

- `v1` prefix is the cache-version invalidator (bump → effective flush).
- `kinds_sorted_csv` included because different `kinds` filters → different result sets.
- **Cache is global**, not per-user. Per-user state lives in `discovery_search_history`.
- **Cache post-ACL `SearchResult[]`** (canonical domain shape), not raw provider JSON. Future ACL evolution invalidates by version-prefix bump.

Per-source TTL:
- MusicBrainz: 24h (canonical metadata stable)
- Last.fm: 12h (tags + counts drift slowly)
- Deezer: 6h (catalog stable; popularity drifts)
- SoundCloud: 1h (leak/unreleased surface is time-sensitive)

New dependencies + infra:
- `redis>=5.0` (native asyncio: `redis.asyncio.Redis`)
- `redis:7-alpine` service in `docker-compose.yml`
- Prod: managed Redis (Upstash free-tier likely; 10k commands/day comfortably above solo + small friends pool)
- `REDIS_URL` env var; `Settings.redis_url: RedisDsn` in `platform/config.py`

Failure mode: Redis outage = cache-miss-falls-through-to-live-provider. Search degrades to "uncached", doesn't fail.

### 3.5 Bounded context name

**Locked: `discovery`.** Parallels `catalog` / `library` / `playback` / `metadata`. Spec name: **`discover-music-v1`** (17 chars).

Path layout:
- `services/api/src/altune/domain/discovery/` — `SearchResult`, `SearchQuery`, `Confidence`, `ResultKind`, `Artist`, `Album`, `Playlist`, `SearchHistoryEntry`
- `services/api/src/altune/application/discovery/` — `SearchProvider` port, `QueryCache` port, `SearchMusic` use case
- `services/api/src/altune/adapters/outbound/discovery/` — `deezer/`, `musicbrainz/`, `soundcloud/`, `lastfm/`, `cache/redis_cache.py`
- `services/api/src/altune/adapters/inbound/http/discovery/router.py` — `GET /v1/discovery/search`, `POST /v1/discovery/clicks`
- `apps/mobile/src/features/discover/` — vertical slice

### 3.6 Query normalization rules

Applied in order, both to user query AND to provider results before JW comparison. Lives in `application/discovery/normalize.py`.

1. Unicode NFKC normalization (fullwidth → halfwidth, ligatures decomposed)
2. Lowercase (after NFKC)
3. Strip diacritics — `unicodedata.normalize('NFD', s)` then drop combining marks. `Beyoncé` → `beyonce`.
4. Drop bracketed suffixes — `(Remastered 2009)`, `[Deluxe Edition]`, `(feat. X)`. Captured separately as `features: [str]` and `edition: str | None` if needed for display.
5. Normalize feature notation — `feat.` / `ft.` / `featuring` / `with` → unified `feat` token before bracket-strip.
6. Strip leading article on artist names — `the smiths` ↔ `smiths`, `los lobos` ↔ `lobos`. Articles in the actual word stay (`the the`).
7. Collapse punctuation + whitespace — `&` → `and`; strip apostrophes/periods/commas; collapse spaces.
8. Trim.

Worked example: `"The Beatles - Let It Be (Remastered 2009)"` → `"beatles let it be"` + side-info `{edition: "Remastered 2009"}`.

Deferred to v2 (documented as Risks in the spec):
- Romanization (Japanese / Korean / Arabic)
- Phonetic matching (Soundex / Metaphone)
- Typo correction
- Non-Latin scripts when provider only indexes Latin

### 3.7 Response shape (wire contract)

Every downstream spec (`play-from-search`, `add-result-to-library`, `subscribe-to-artist`) depends on this contract.

```jsonc
GET /v1/discovery/search?q=<text>&kinds=artist,album,track,playlist&limit=25

200 OK
{
  "query": "the beatles",
  "query_norm": "beatles",
  "results": [
    {
      "kind": "track",
      "title": "Let It Be",
      "subtitle": "The Beatles",
      "image_url": "https://...",
      "confidence": "high",
      "sources": [
        {"provider": "musicbrainz", "external_id": "9c9f1380-...", "url": "https://musicbrainz.org/recording/..."},
        {"provider": "deezer",      "external_id": "3135556",     "url": "https://deezer.com/track/3135556"}
      ],
      "extras": {
        "isrc": "GBAYE0601477",
        "duration_seconds": 243,
        "album": "Let It Be",
        "year": 1970,
        "preview_url": null
      }
    }
  ],
  "providers": [
    {"provider": "musicbrainz", "status": "ok",      "result_count": 18, "latency_ms": 412},
    {"provider": "deezer",      "status": "ok",      "result_count": 23, "latency_ms": 178},
    {"provider": "lastfm",      "status": "ok",      "result_count": 14, "latency_ms": 305},
    {"provider": "soundcloud",  "status": "timeout", "result_count": 0,  "latency_ms": 1500}
  ],
  "partial": true,
  "cache": {"hit": false, "fetched_at": "2026-05-27T15:23:11Z"}
}
```

Load-bearing shape choices:
- **`sources: []` array** — open-closed against adding/removing providers; multi-source signal visible to UI.
- **`extras: {}` dict per-kind** — tolerant-reader (ADR-0006 precedent); mobile reads only what it knows.
- **`providers: []` per-source status** — "we checked X, Y; Z was unavailable" UI.
- **`preview_url` reserved-null v1** — `track-playback` future spec lights it up without a breaking change.
- **No cursor v1** (see §3.8).
- **`query_norm` echoed** — client may show "results for: …" if normalization differed.

### 3.8 Pagination

**Locked: Top-N only v1.** 25 per source × 4 sources = 100 raw, post-dedup ~30–50 merged. No cursor / next-page concept.

If v2 telemetry shows users hit the bottom and want more: one new query param `?cursor=`, server signs/unsigns per-source offsets, scatter-gather reruns with offsets. Contained.

Named trade-off: rare/long-tail results not in any provider's top-25 are invisible v1. Mitigation lives in the confidence model — rare-but-correct hits (high JW + MB-prior) outrank pop noise even at small N.

### 3.9 Search trigger (UX)

**Locked: Submit-only v1 + explicit v1.1 commitment for as-you-type (hybrid).**

v1 = user types, presses Enter / Search icon. One query per intent. Ships fast.
v1.1 = debounced as-you-type when bar is focused + ≥3 chars + after first submit. Adds AbortController-style cancellation pipeline + stale-response guards.

### 3.10 URL-paste resolution

**Locked: YES, v1.** If `q` matches `^https?://(deezer|soundcloud|musicbrainz|last\.fm)\.\w+/...`, skip scatter-gather, route directly to that source's `lookup_by_url(url)` adapter method, return a single high-confidence result. High-value UX, especially for the SC-leak-share flow.

### 3.11 Per-user search history

**Locked: YES, v1.**

Table: `discovery_search_history(id, user_id, query_norm, executed_at, result_clicked_signature)`. Ring-buffer: keep latest 50 per user; on insert past 50, oldest drops. Empty-state on Discover shows last 10. Tap → re-runs that query.

### 3.12 Telemetry posture

**Locked: Log raw normalized query + hash.** ADR-0007 documents the privacy stance + commitment to revisit when the user base grows beyond friends.

Events:
- `search_performed` — `user_id`, `query_norm` (raw), `query_norm_hash`, `kinds`, `providers_called`, `result_count`, `total_latency_ms`, `partial`
- `search_provider_status` — `provider`, `status`, `result_count`, `latency_ms`
- `search_result_clicked` — `query_norm_hash`, `result_position`, `result_confidence_bucket`, `result_kind`, `chosen_source`
- `search_zero_results` — when all sources returned 0

**Success metric for "did we fix legacy accuracy?"**: click-through rate on top-3 results, segmented by `confidence` bucket. High-confidence top-3 CTR significantly > low-confidence's = the confidence model has signal.

### 3.13 Result ranking + domain events + v2 accuracy backlog

**Ranking** (multi-criteria, in order):
1. `confidence` DESC (high > medium > low)
2. Multi-source agreement (`len(sources) > 1`)
3. Per-source prior (MB > Deezer > Last.fm > SoundCloud)
4. Final tie-break: alphabetical on `(subtitle, title)`

**Domain events** (in `domain/discovery/events.py`):
- `SearchPerformed(query_norm, user_id, executed_at, total_results, partial)` — raised by `SearchMusic` use case
- `ResultClicked(user_id, query_norm, result_signature, position, confidence)` — raised on `POST /v1/discovery/clicks`

`result_signature` = stable hash of `(kind, normalized title, normalized subtitle)` — lets clicks be correlated without persisting full `SearchResult`s.

**Accuracy v2 backlog** (NOT in v1; future ADRs / specs):
1. Typo correction (`pyspellchecker`)
2. Synonym maps
3. Romanization (`pykakasi`, `korean-romanizer`)
4. Phonetic matching (Metaphone / Soundex)
5. User-signal re-ranking (past clicks boost results for that user)
6. Embedding similarity (small sentence-transformer; ML-infra ADR territory)

## 4. Additional Claude-call details (small but spec-relevant)

### 4.1 Click-tracking endpoint

```
POST /v1/discovery/clicks
{"query_norm": "beatles", "result_signature": "track:let-it-be:the-beatles", "position": 1, "confidence": "high"}
202 Accepted
```

- Fire-and-forget from mobile; persists to `discovery_search_clicks` for analytics; no body returned.
- **Idempotency**: dupes (same `user_id + query_norm + result_signature` within 60s) deduped server-side.
- **Abuse cap**: 100 clicks/min/user.

### 4.2 `SearchProvider` port signature

```python
class SearchProvider(Protocol):
    @property
    def name(self) -> str: ...  # 'deezer' | 'musicbrainz' | 'soundcloud' | 'lastfm'

    async def search(
        self,
        query: str,
        kinds: frozenset[ResultKind],
        limit: int,
    ) -> ProviderSearchResponse: ...

    async def lookup_by_url(self, url: str) -> ProviderSearchResult | None: ...
```

- `ProviderSearchResponse` carries already-translated `ProviderSearchResult`s + per-provider status (`ok`/`timeout`/`error`/`rate_limited`).
- **Translation lives in the adapter.** The use case never sees provider DTOs.

### 4.3 Test strategy

Per [services/api/CLAUDE.md](../../services/api/CLAUDE.md) test setup:
- **Unit (domain + application):** `InMemorySearchProvider` returns canned responses; tests assert dedup, ranking, confidence buckets, multi-source merging, URL-paste routing.
- **Adapter integration:** each provider tested against `respx`-mocked HTTP responses (real provider JSON shapes captured from one-shot live calls, frozen as fixtures). No live calls in CI.
- **E2E:** `httpx.AsyncClient` against in-process app + `respx` mocks of all 4 providers + `testcontainers` Redis. One happy-path test asserts merged response shape end-to-end.
- **Confidence calibration:** hypothesis-property-test `normalize_for_match()` + JW similarity over generated artist/title pairs.

### 4.4 Tolerant-reader posture on provider responses

- **Required fields** missing → drop the result + log `provider_response_malformed`. Don't fail the whole search.
- **Optional fields** missing → null in `SearchResult`.
- **Unknown fields** → ignored.

### 4.5 Mobile offline + concurrency

- Discover with no network: empty-state + offline indicator. No "show last cached results" v1.
- Submit-only means ≤1 in-flight `/v1/discovery/search` at a time (button disabled while loading).
- v1.1 as-you-type: AbortController cancels prior in-flight; `httpx` propagates cancellation through TaskGroup server-side.

### 4.6 Cost model

All 4 providers $0/month at solo + small friends pool. Redis = Upstash free tier (10k commands/day).

### 4.7 Migration / rollback

v1 has no feature flags; rollback = redeploy prior build. New tables (`discovery_search_history`, `discovery_search_clicks`) are additive. Redis is the new infra piece — if it fails, the design degrades gracefully (live providers still hit; just no cache).

## 5. Operational pre-spec checklist (for ADR-0007 Implementation notes)

1. ~~Register the altune SoundCloud developer app~~ — superseded; SoundCloud via yt-dlp (per the strategy revision above) requires no registration.
2. Register a Last.fm API account → `LASTFM_API_KEY` (and `LASTFM_SHARED_SECRET` reserved for future write endpoints).
3. Choose prod Redis host (Upstash free-tier likely) → `REDIS_URL` for prod env.
4. Confirm MusicBrainz registered User-Agent string format + contact email.
5. Capture frozen-fixture JSON responses from each of the 4 providers for `respx` mocking in CI (one happy-path per (provider, kind)).

## 6. Spec-level open items (not brainstorm decisions — `/feature-spec` resolves)

- Mobile UX surface (tab vs modal vs persistent bar) — `ux-reviewer` gates.
- Exact `N` shown in empty-state (locked at 10; spec-level calibration).
- Region / catalog-locking risk (Deezer catalog varies by country) — note in spec Risks.
- NSFW / explicit-content filtering (SoundCloud especially) — note in spec Risks; v1 passes through.
- Track-preview / `preview_url` exposure — reserved-null v1.
- Future spec dependencies: `play-from-search` (uses `sources[]`), `add-result-to-library` (uses `sources[] + extras.isrc`), `subscribe-to-artist` (uses MB artist external_id). Locked shape accommodates all three.

## 7. Locked summary

| Decision | Locked |
|---|---|
| Source set v1 | Deezer + MusicBrainz + SoundCloud + Last.fm |
| Aggregation | Scatter-gather, `asyncio.TaskGroup`, 1500ms/source, 2000ms total, partial-results |
| Resiliency | Circuit breaker (5→30s), no retries, bulkhead per-source httpx client |
| Confidence/dedup | ISRC + JW≥0.92 + per-source priors tie-break, `{high, medium, low}` |
| Similarity lib | `rapidfuzz` |
| Caching | Redis, ADR-level, per-source TTL, cache post-ACL shape, version-prefix invalidation |
| Bounded context | `discovery`; spec name `discover-music-v1` |
| Result types v1 | Artist + Album + Single (Track) + Playlist |
| SoundCloud auth | yt-dlp `scsearch:` extraction (no API key, tracks-only; revised 2026-05-27 due to Artist Pro gating) |
| API endpoint | `GET /v1/discovery/search?q=&kinds=&limit=25`; `POST /v1/discovery/clicks` |
| Query normalization | 8-step rule list in §3.6 |
| Response shape | `sources: []` + `extras: {}` + `providers: []` + reserved `preview_url: null` |
| Pagination | Top-N only v1, cursor deferred |
| Result ranking | confidence DESC → multi-source → per-source prior → alpha |
| Domain events | `SearchPerformed`, `ResultClicked` |
| Accuracy v2 backlog | Documented; v1 does not commit to any |
| Search trigger | Submit-only v1 + v1.1 hybrid commitment |
| URL-paste resolution | Yes, v1 |
| Search history | Yes, v1 (ring-buffer 50, empty-state shows 10) |
| Telemetry | Log raw `query_norm` + hash; ADR documents privacy commitment |
| Mobile UX surface | Deferred to spec |

## 8. Next steps

1. **`/adr-write`** → `docs/adr/0007-unified-music-search.md`. Captures all locked decisions above; bundles Redis adoption (precedent: ADR-0006 bundled mobile auth into the auth-mode ADR).
2. **`/feature-spec discover-music-v1`** → `docs/specs/discover-music-v1/spec.md`. References ADR-0007. Auto-appends `discover-music-v1` to commitlint scope-enum and the new domain terms to `docs/ubiquitous-language.md` (moving `Artist` / `Album` / `Playlist` from Future to Canonical, adding `SearchResult` / `SearchQuery` / `Confidence` / `ResultKind` / `SearchHistoryEntry`).
