# Discovery context — router

The search pipeline (`service/`), providers (`adapters/providers/`), and the value objects and ports they share. Pipeline shape + ranking key: `ARCHITECTURE.md`. Eval harness usage: `cmd/discoveryeval/CLAUDE.md`.

Layout:

- `domain/` — `SearchResult`, the enrichment value objects, telemetry events, `FeaturedArtist`, identity read-models, `favorite.go` (the `Favorite` value object and its key derivation).
- `ports/` — provider, artwork, cache, identity-store and catalog-ownership interfaces.
- `service/` — `search.go` (the `Service` orchestrator), `merge.go` / `rank.go` / `diversity.go` (the Merge→Rank→reshape core), `enrich/` (detail-open enrichers), `eval/` (offline harness cores, including `library_corpus.go`'s frozen-corpus load/save and `parallel.go`'s `runParallel[T]` — the shared fan-out spine the five `Run*Eval` modes drive with their own worker closures), and `album_normalize.go` (the pure album-slice helpers — dedup, release-date sort, year-fill — split out of `get_artist_content.go`'s fetch orchestration).
- `adapters/` — `providers/` (one file per provider), `cache/`, `persistence/`, `handler/`, `catalogbridge/` (the read-only seam onto catalog for ownership stamping and track-number fill). In `cache/`, the unexported `redisJSON` (`redis_json.go`) holds the single `disabled()` nil-guard, the shared `"1"` negative sentinel, and the `getJSON[T]`/`setJSON` helpers embedded across every Redis cache — the result cache routes through the generic `RedisNameKeyedCache[T]`; artwork (confidence-gated write), identity (fan-out) and enrichment (non-hashed `kind:mbid` positive key) stay bespoke but reuse the guard.

## Rules

Ranking and merge:

- Never add a word bank, special-case list or tuned threshold to fix a query — fix the algorithm.
- Any rank-affecting change must re-clear `discoveryeval` before it ships.
- A/B on an identical deterministic sample (`-limit`, no `-random`); never compare across random samples.
- Keep `rankLess`'s multi-source comparison unconditional — gating it broke transitivity.
- Never re-wire popularity into the search path.
- Never add a fuzzy-title threshold to `Merge`.
- A later merge must never downgrade an entity's already-proven identity.

Identity and artwork:

- A name-searched image is labelled provisional, never identity. There is no same-name fallback.
- Identity-only resolvers run before any name-based resolver in the chain.
- Never write artwork at lower confidence over an existing positive entry.
- Never key identity on names — provider ids only.
- `MBIDIndex` is cache-only; never call MusicBrainz from the search path.
- A search-backed artwork resolver accepts a hit only when the candidate's own text covers the requested title (and artist, when one is given) — a blank cover beats a confidently wrong one.

Degradation:

- Every detail-open enricher returns an empty value and a nil error — never a surfaced failure.
- Every `Empty*Enrichment` must return non-nil slices and maps.
- Never cache partial consensus verdicts, and never cache MusicBrainz errors or timeouts.
- A lookup, cache-write or trim failure must never fail the search.
- Identity verification is fail-open — never drop an edge on a fetch failure.
- A panicking provider adapter must never kill the process.

Adapters:

- Never wire YouTube Music by-name gathering into the search path (artwork only).
- A second 401 handler must not wipe a credential the first one just obtained. The single owner of this dance is the generic `cachedResolver[T]` in `adapters/providers/resolver.go`; the five providers pass only their own `resolve`/`valid` — never re-hand-roll the lock, singleflight, detached timeout or invalidate.
- In `cachedResolver`, a zero expiry from `resolve` means never-expires; a provider that has an expiry concept must always return a non-zero expiry, or its credential caches forever.
- Never block the hot search path on Deezer lyrics.
- Never add a `user_id` filter to `related_tracks_repo`'s cross-user scan — it is deliberate.
- Never let a malformed row payload fail a whole batch; skip the row.
- An xref upsert merges, never replaces.

Ownership and shaping:

- Stamp ownership at the wire edge only; the ranking path never sees a user's library.
- Keep `ports.OwnershipKey` the single normalizer both the bridge and the stamper use.
- An ownership lookup failure degrades to an unstamped result — never a failed search.
- Fill a track number off the request path, on a detached context.
- `BuildBlendedSlate` reads the ranked order and never re-ranks; the top result is excluded from its own section.

Favorites:

- Apply the Favorites lift per request, after the shared result cache — that cache is keyed by query, not by user.
- `domain.FavoriteKey` is the only place a Favorite's key is derived; the wire echoes it as `favorite_key` and clients never recompute it.
- A favorited artist lifts their tracks and albums too; a favorite is only lifted inside `favoriteLiftWindow`, never from the far tail.
- A favorites read failure leaves the ranked order untouched — it never fails the search.

Telemetry:

- One event per search, not per page.
- Exploration shuffles a copy; never mutate the cached list.
- Never persist identity or telemetry on the request path — detached context, off the hot path.
- `search_performed` is server-emitted; reject it at `POST /events`. `results_shown` is client-emitted and accepted there.

Endpoint limit policies (deliberately not one call):

- The three search-endpoint `limit` params carry three different, test-pinned overflow policies and live in three different layers — they are not foldable into content's `clampLimit` (`<=0->def`, `>max->max`). `suggest` resets on overflow in the handler (`<=0` or `>10` -> `5`, so `limit=11` -> `5`, never clamp-to-cap) via `limitResetOnOverflow`. `search` only defaults `<=0->20` in the handler; the `>50` cap is a domain invariant rejected with 400 by `NewSearchQuery` (`limit=51` -> 400), never clamped up in the adapter. `search-history` passes the raw limit through; the `<=0->10` default is a service concern (`service/list_history.go`) and there is no upper cap. Routing `search`/`history` through a handler helper would drag domain/service policy into the adapter — keep each where its layer owns it.
