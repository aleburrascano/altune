# Ubiquitous language

Shared vocabulary across code, tests, conversation, and documentation. When a term is used in `services/api/src/altune/domain/`, it lives here with a precise meaning.

Reference: `[vault: wiki/concepts/Ubiquitous Language.md]`, `[vault: wiki/concepts/Domain-Driven Design.md]`.

## Rules

1. **One term, one meaning.** If "playlist" means two different things in two contexts, name them differently (e.g., `UserPlaylist` vs. `SmartPlaylist`).
2. **Code matches glossary.** Class names, method names, variable names use these terms verbatim. The `terminology-drift` hook flags drift.
3. **Glossary entries match code.** If a term appears here but not in the code, either delete it (premature) or build the type (overdue).
4. **Defined per bounded context** when a term diverges. Most terms are global; some need context-qualified entries (see "Per-context overrides" below).

## Adding a term

When `/feature-spec` or domain modeling introduces a new term:
1. Add it here in the same commit (the `terminology-drift` hook will flag if you don't).
2. Use the format below.
3. If the term overrides a global definition in a specific context, add a "Per-context overrides" entry.

## Format

```
- **TermName** — definition in 1–3 sentences. Cross-link to vault if applicable.
```

---

## Glossary

### Canonical (terms with corresponding code)

- **Track** — a single audio recording with metadata (title, artist, optional album, optional duration). Aggregate root of the **catalog** bounded context. Identity is a `TrackId` (UUID); equality and hashing are by id. Owned by a user (`UserId`). Defined in `services/api/src/altune/domain/catalog/track.py`. Introduced by spec `docs/specs/view-library/spec.md`.
- **ResultKind** — discriminator on a `SearchResult` indicating what kind of music it represents: `artist`, `album`, `track`, or `playlist`. Wire-serialized as a lowercase string. Defined in `services/api/src/altune/domain/discovery/result_kind.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **Confidence** — three-level enum on a `SearchResult` indicating how confident the dedup engine is that the merged entry refers to one canonical recording: `high` (ISRC match or JW ≥ 0.92), `medium` (JW ∈ [0.85, 0.92) with per-source-prior tie-break), `low` (standalone provider result). Comparable: `HIGH > MEDIUM > LOW`. **Display-only since the ADR-0007 ranking-overhaul addendum (2026-05-28)** — a trust badge on the client, no longer a ranking term; relevance (RRF + exact-match boost in `fuse_and_rank`) drives order. Defined in `services/api/src/altune/domain/discovery/confidence.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SourceRef** — one provider's reference to a merged `SearchResult`, carrying `(provider: ProviderName, external_id: str, url: str)`. A multi-source-merged result holds a tuple of SourceRefs. Defined in `services/api/src/altune/domain/discovery/source_ref.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SearchResult** — the merged + dedup'd discovery result entry on the wire. Carries `(kind, title, subtitle?, image_url?, confidence, sources: tuple[SourceRef, ...], extras: Mapping[str, object])`. Multi-source-merged results carry multiple SourceRefs. Invariants: title non-empty, sources tuple non-empty. Defined in `services/api/src/altune/domain/discovery/search_result.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SearchQuery** — validated user search input crossing the API boundary into the use case. Carries `(raw, query_norm, kinds: frozenset[ResultKind], limit)` with invariants enforcing non-empty `raw`, non-empty `kinds`, `1 <= limit <= 50`. Defined in `services/api/src/altune/domain/discovery/search_query.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **ProviderStatus** — per-provider outcome of one scatter-gather call: `ok`, `timeout`, `error`, `rate_limited`, `circuit_open`. Surfaced on the wire's `providers[]` array per AC#5/5a/5b/6. Defined in `services/api/src/altune/domain/discovery/provider_status.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SearchHistoryEntry** — per-user persisted search-history row, aggregate root of the search-history surface. Identity is a `SearchHistoryEntryId` (UUID). Carries `(user_id, query, query_norm, executed_at, result_clicked_signature?)`. Ring-buffer trim + distinct-recent reads are repository concerns (slice 37). Defined in `services/api/src/altune/domain/discovery/search_history_entry.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SearchClick** — per-user persisted click on a discovery result, aggregate root of the click-tracking surface. Identity is a `SearchClickId` (UUID). Carries `(user_id, query_norm, result_signature, position, confidence, clicked_at)`. Position is non-negative. Sliding-window dedup is a repository concern (slice 40). Defined in `services/api/src/altune/domain/discovery/search_click.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.
- **SearchPerformed** / **ResultClicked** — discovery domain events (past-tense, immutable, carry `occurred_at`). Emitted to logs v1; future analytics persistence specs may consume them. Defined in `services/api/src/altune/domain/discovery/events.py`. Introduced by spec `docs/specs/discover-music-v1/spec.md`.

- **Artist** — a creator of tracks, surfaced in the discovery context as a `SearchResult` with `kind = ResultKind.ARTIST` carrying name in `title`, optional disambiguator in `extras` (year/MBID). Promoted from Future to Canonical by `discover-music-v1`. A future spec may extract `Artist` into its own value object once a write-side library surface (subscribe-to-artist, etc.) needs identity beyond `(provider, external_id)`.
- **Album** — a grouping of tracks released together by an artist, surfaced as a `SearchResult` with `kind = ResultKind.ALBUM` carrying title + artist subtitle + year in `extras`. Promoted from Future to Canonical by `discover-music-v1`. Becomes its own type when a future spec needs distinct identity (e.g., track-album navigation).

### Future (illustrative — to be added when the spec that introduces them ships)

- **Library** — a user's personal collection. Each user has exactly one library. The library references tracks from the catalog; it does not own them.
- **Queue** — the runtime playback sequence. Ephemeral by default; persisted only when saved as a Playlist.
- **Play** — the event of a track being listened to (registered at threshold, e.g., 30s or 50% of duration).
- **Favorite** — a user-applied boolean marker on a track within their library.

---

## Per-context overrides

When the same term means different things in different contexts, define each:

```
- **TermName** (in <Context>) — context-specific meaning.
```

_(empty — populated when context divergence happens)_

---

## Anti-patterns

- **Synonyms drift** — "song" and "track" used interchangeably. Pick one; ban the other.
- **Implementation leakage** — "TrackRow" or "TrackDTO" in glossary. Those are infrastructure, not domain.
- **Vague entries** — "User: a person who uses the app." Useless. If a term is in the glossary, it earns its place with a precise definition.
- **Stale entries** — terms that were once in the code but were renamed/removed. Delete or mark deprecated.

## Banned terms

Terms that **must not** appear in altune code (caught by the `terminology-drift` hook and by code review):

- **Song** — synonym of `Track`. The legacy `music-manager` used "song" as its primary noun (`songs` table, `Song` class). altune uses `Track` exclusively. The forthcoming `migrate-songs-v1` spec is the one place "song" appears, and only as the *name of the source data* during the import — never as a type or column in altune.
