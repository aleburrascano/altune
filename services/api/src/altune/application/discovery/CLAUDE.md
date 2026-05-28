# discovery application — bounded-context local rules

Use cases + ports for the unified music search. `SearchMusic` is the load-bearing use case — scatter-gather over 4 providers, dedup + rank, persist history. `RecordClick`, `ListSearchHistory` are smaller surface use cases. All adapter wiring lives in [services/api/src/altune/platform/wiring.py](../../platform/wiring.py).

## Key terms

- **SearchProvider** — `Protocol` adapters implement (deezer, musicbrainz, soundcloud, lastfm) [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\ports.py]. Two methods: `search(query, kinds, limit)` returns `ProviderSearchResponse`; `lookup_by_url(url)` returns `SearchResult | None`.
- **QueryCache** — `Protocol` the Redis adapter implements. `get(provider, query_norm, kinds)` returns `(results, fetched_at) | None`; `set(... ttl)`. Errors degrade to None (caller falls through to live).
- **ClickInsertOutcome** — `(inserted: bool, deduped_against_id: UUID | None)`. Sliding-window dedup result from `SearchClickRepository.insert_if_outside_window`.
- **Scatter-gather** — `asyncio.gather(*tasks)` with per-task try/except so siblings survive any one provider's failure [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\search_music.py#L122-L139].
- **result_signature** — `sha256(f"{kind}|{normalize(title)}|{normalize(subtitle)}")[:12]` [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\record_click.py#L46-L51]. Diacritic variants collapse to the same hash.

## Patterns specific here

- **SearchMusic instance is request-scoped** in the router today — built fresh per request. Circuit breakers live on the instance, so they DON'T persist across requests in v1. Acceptable trade-off; a future refactor moves breakers to app.state if telemetry shows resets thrashing.
- **`_call_provider_with_cache` is the only entry the gather tasks touch** — cache check, then `_call_provider`, then optional cache write. All three operations swallow exceptions per the partial-results contract.
- **Cache TTLs are per-source** — MB 24h, Last.fm 12h, Deezer 6h, SoundCloud 1h. Defined at module level in `_DEFAULT_TTLS` [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\search_music.py#L43-L48]; overridable via the `cache_ttls` field for tests.
- **Rate-limited NEVER counts toward the circuit breaker.** OK = `record_success()`; ERROR/TIMEOUT = `record_failure()`; RATE_LIMITED = no-op. Per AC#5b. The TIMEOUT path also calls `record_failure()`.
- **`url_router.match_provider(query)` is the only URL-detection gate.** SearchMusic's `_execute_url_lookup` is called when match returns a provider; unsupported hosts fall through to scatter-gather text search (AC#10a).
- **History persist is best-effort.** Wrapped in try/except; failures log `search_history_persist_failed` and the search still returns 200.

## Known gotchas

- **`from __future__ import annotations` + dataclass field types in TYPE_CHECKING** can trip ruff's I001 / TC003 lint when the import is only used in a field annotation. Keep `Sequence`, `Mapping`, `QueryCache`, `ProviderName`, `SearchProvider`, `SearchHistoryRepository`, `SearchResult` etc. in the `TYPE_CHECKING` block — they resolve at runtime via string annotations.
- **`dedup_and_rank` uses `rapidfuzz.distance.JaroWinkler`** [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\application\discovery\dedup.py#L16-L16] which mypy lacks stubs for; the `rapidfuzz.*` override in pyproject.toml handles this in batch mypy but per-file mypy hook will complain. Tolerate.
- **JW boundary cases**: 0.85 (merge with MEDIUM), 0.92 (merge with HIGH). Test parametrization explicitly hits 0.84 / 0.85 / 0.91 / 0.92 — don't simplify those tests away.
