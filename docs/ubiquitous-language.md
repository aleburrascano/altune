# Ubiquitous language

Shared vocabulary across code, tests, conversation, and documentation. When a term is used in `services/api/src/altune/domain/`, it lives here with a precise meaning.

Reference: `[vault: wiki/concepts/Ubiquitous Language.md]`, `[vault: wiki/concepts/Domain-Driven Design.md]`.

This file stays **definitional** — what a term IS (shape, invariants, code location). Deep implementation rationale for discovery/provider concepts lives in the `okf/` knowledge bundle (see `CLAUDE.md` OKF context) and is fetched on demand — don't re-inflate entries with design narrative that belongs there. Spec provenance ("introduced by X") lives in `docs/specs/`, not here.

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

- **Track** — a single audio recording with metadata (title, artist, optional album/year/genre/track_number/album_artist/isrc/duration/artwork_url, `audio_ref`) plus an `AcquisitionStatus`. Aggregate root of the **catalog** bounded context; identity is `TrackId` (UUID); owned by a `UserId`. Defined in `services/api/src/altune/domain/catalog/track.py`.
- **Playlist** — a user-created, named, ordered collection of tracks. Aggregate root of the **catalog** bounded context; identity is `PlaylistId` (UUID); owned by a `UserId`. Carries `name` (non-empty, ≤100 chars), timestamps, and an ordered tuple of `PlaylistTrack` entries — invariants: non-empty name, contiguous 0..N-1 positions, no duplicate track_ids. Defined in `services/api/src/altune/domain/catalog/playlist.py`.
- **PlaylistTrack** — value object for a track's membership in a playlist: `track_id: TrackId` + `position: int` (0-indexed). Lives inside the Playlist aggregate, not standalone. Defined in `services/api/src/altune/domain/catalog/playlist.py`.
- **AcquisitionStatus** — lifecycle of a saved track's audio acquisition: `pending` (not yet acquired), `ready` (acquired, streamable), `failed` (acquisition failed or file went missing; requires `failure_reason`). Wire-serialized lowercase. `audio_ref ↔ status` is a bidirectional invariant on Track: `audio_ref` set requires `ready`; `failed` requires `failure_reason` and clears `audio_ref`. Defined in `services/api/src/altune/domain/catalog/acquisition_status.py`.
- **ResultKind** — discriminator on `SearchResult`: `artist`, `album`, `track`, or `playlist`. Wire-serialized lowercase. Defined in `services/api/src/altune/domain/discovery/result_kind.py`.
- **Confidence** — three-level enum on `SearchResult` for dedup-merge certainty: `high` (ISRC match or JW ≥0.92), `medium` (JW ∈[0.85,0.92) with per-source-prior tie-break), `low` (standalone result). Comparable: HIGH > MEDIUM > LOW. **Display-only since ADR-0007** — a client trust badge, not a ranking input; relevance (RRF + exact-match boost) drives order. See okf: `discovery/merge-dedup`. Defined in `services/api/src/altune/domain/discovery/confidence.py`.
- **SourceRef** — one provider's reference to a merged `SearchResult`: `(provider, external_id, url)`. A multi-source result holds a tuple of these. Defined in `services/api/src/altune/domain/discovery/source_ref.py`.
- **SearchResult** — the merged, dedup'd discovery result on the wire: `(kind, title, subtitle?, image_url?, confidence, sources: tuple[SourceRef,...], extras)`. Invariants: non-empty title, non-empty sources. Defined in `services/api/src/altune/domain/discovery/search_result.py`.
- **SearchQuery** — validated search input crossing into the use case: `(raw, query_norm, kinds: frozenset[ResultKind], limit)`. Invariants: non-empty `raw`, non-empty `kinds`, `1 ≤ limit ≤ 50`. Defined in `services/api/src/altune/domain/discovery/search_query.py`.
- **ProviderStatus** — per-provider outcome of one scatter-gather call: `ok`, `timeout`, `error`, `rate_limited`, `circuit_open`. Surfaced on the wire's `providers[]` array. See okf: `discovery/scatter-gather`. Defined in `services/api/src/altune/domain/discovery/provider_status.py`.
- **SearchHistoryEntry** — per-user persisted search-history row. Identity `SearchHistoryEntryId` (UUID); carries `(user_id, query, query_norm, executed_at, result_clicked_signature?)`. Defined in `services/api/src/altune/domain/discovery/search_history_entry.py`.
- **SearchClick** — per-user persisted click on a discovery result. Identity `SearchClickId` (UUID); carries `(user_id, query_norm, result_signature, position, confidence, clicked_at)`. Defined in `services/api/src/altune/domain/discovery/search_click.py`.
- **TrackAddedToLibrary** — catalog domain event (past-tense, immutable): `occurred_at`, `track_id`, `user_id`. Emitted when a track is freshly saved to a library; a dedup hit emits none. Defined in `services/api/src/altune/domain/catalog/events.py`.
- **SearchPerformed** / **ResultClicked** — discovery domain events (past-tense, immutable, carry `occurred_at`). Logged in v1. Defined in `services/api/src/altune/domain/discovery/events.py`.
- **InteractionEvent** — the unified behavioral-telemetry envelope persisted to `discovery_events`: `(occurred_at, user_id, type, query_norm?, search_id?, payload)`. `EventType` is closed: `search_performed`, `results_shown`, `result_clicked`, `play`, `skip`, `completed`, `library_add`, `wrong_album`; new fields ride in JSONB `payload`. See okf: `discovery/telemetry`. Defined in `services/go-api/internal/discovery/domain/events.go`.
- **SearchId** — the UUID minted per `search_performed`, threaded onto every downstream engagement event so impression→click→play joins back to its search. Required by every show-conditioned metric (CTR@position, MRR/NDCG, counterfactual replay). Defined in `services/go-api/internal/discovery/domain/events.go`.
- **ResultSignature** — the server-computed stable identity of a result, `(kind, normalized title, normalized subtitle)`, echoed on engagement events. The cross-query, cross-provider join key for attribution. Computed in `services/go-api/internal/discovery/adapters/handler/discovery_handler.go`.
- **results_shown** — the visibility-confirmed impression event: emitted once per search when results actually enter the viewport, carrying the shown slate `(result_signature, position, provider, confidence)`. Prerequisite for position-bias correction; distinct from `search_performed`'s server-side "returned" snapshot.
- **EventConsumer** — the Strategy seam behavioral ranking signals plug into: `Name()` + `Signals(ctx, since)`, deriving a signal from the interaction-event stream. A new signal is a new implementation, not a pipeline rewrite. Defined in `services/go-api/internal/discovery/ports/ports_telemetry.go`.
- **SatisfactionConsumer** — the first `EventConsumer`: `play`/`completed` (listen threshold 30s or 50% duration) ⇒ positive per-`result_signature` score; `skip` under 20s dwell ⇒ negative. Gated default-OFF by `BEHAVIORAL_RANKING_ENABLED`. Defined in `services/go-api/internal/discovery/service/behavioral_signals.go`.
- **BehavioralLabel** / **BehavioralCorpus** — a relevance label mined from a query→engagement chain: `completed`/`library_add` ⇒ positive, `wrong_album` ⇒ hard negative. `CorpusBuilder` materializes labels nightly into the eval-harness corpus format. See okf: `discovery/eval-harness`. Defined in `services/go-api/internal/discovery/service/eval/behavioral_corpus.go`.
- **Exploration** — a small fraction (`EXPLORATION_RATE`, default 3%) of searches serve a randomized result order, stamped `exploration: true`. Generates unbiased propensity data for counterfactual eval. OFF by default (`EXPLORATION_ENABLED`). Defined in `services/go-api/internal/discovery/service/search.go`.
- **ReplayCorpus** — the offline counterfactual scorer: scores a candidate ranking against `BehavioralCorpus` without serving it. See okf: `discovery/eval-harness`. Defined in `services/go-api/internal/discovery/service/eval/replay.go`.
- **session_id** — a rotating correlation id on every behavioral event's payload, grouping a search arc (search→click→play). Rotates on app foreground or 30 min idle. Client-managed in `apps/mobile/src/shared/telemetry/session.ts`.
- **abandoned_search** — a `search_performed` with no click, reformulated (same `session_id` fires another search within 60s). Defined in `SessionSignalStore.AbandonedSearches` (`services/go-api/internal/discovery/adapters/persistence/event_repo.go`).
- **event_id** / **two-tier reliability** — label-critical events (`library_add`, `wrong_album`) carry a client-minted idempotency key (`event_id`) via a client outbox; insert dedups on `ON CONFLICT DO NOTHING`. Everything else is fire-and-forget. Dual timestamps: `client_occurred_at` vs. `received_at`. Client outbox in `apps/mobile/src/shared/telemetry/outbox.ts`.
- **discovery_metrics** — nightly rollup table: one row per `(as_of day, metric)` for `zero_result_rate`, `ctr`, `tail_noise_top5_avg`, `searches`. Aggregates only, no `user_id`. Feeds the `ALERT_ZERO_RESULT_THRESHOLD` coverage alert. Defined in `services/go-api/internal/discovery/adapters/persistence/metrics_rollup_repo.go`.

- **Artist** — a creator of tracks, surfaced as a `SearchResult` with `kind = ResultKind.ARTIST`, name in `title`, optional disambiguator (year/MBID) in `extras`.
- **Album** — a grouping of tracks released together, surfaced as a `SearchResult` with `kind = ResultKind.ALBUM`, title + artist subtitle + year in `extras`.
- **FeaturedArtist** — a guest/`feat.` credit on a track: immutable value object carrying `name`, optional `mbid`, optional `deezer_id`, `role` (only `featured` populated in v1). Not an entity — no lifecycle beyond membership on a `Track`. Grouping identity is `COALESCE(mbid, deezer_id, normalized name)`. Wire form in `SearchResult.Extras["featured_artists"]`; persisted in catalog via `featured_artists` + `track_featured_artists`. See ADR-0019. Defined in `services/go-api/internal/discovery/domain/` (wire) and `services/go-api/internal/catalog/domain/` (persisted).
- **FeaturedArtistResolver** — discovery service merging MusicBrainz `artist-credit[1..]` (name + MBID + joinphrase) with Deezer `/track/{id}` `contributors` (explicit `Featured` role + Deezer artist id) into an ordered `[]FeaturedArtist` — MB-primary, Deezer fills gaps. Used by the live search/detail path and by the catalog backfill (via port + bridge adapter), keyed on ISRC.
- **EntityResolutionTier** — how two results were identified as the same entity during dedup: `mbid`, `isrc`, `bridge` (cross-provider id match via MB url-relations), or `none`. Identifier-based only — no text/duration similarity. See okf: `discovery/merge-dedup`. Defined in `services/go-api/internal/discovery/domain/types.go`.

- **MBEnrichment** — immutable detail-screen enrichment from MusicBrainz: `mbid`, curated `genres`, `year`, `rating`+`rating_votes`, `primary_type`+`secondary_types` (album only), `external_ids`, HD `artwork_url`. Not persisted — live read on detail-open. See okf: `discovery/enrichment`. Defined in `services/go-api/internal/discovery/domain/`.
- **MetadataEnricher** — outbound port: `(ResultKind, MBID) → MBEnrichment`. Implemented by the MusicBrainz adapter. Defined in `services/go-api/internal/discovery/ports/`.
- **EnrichmentCache** — outbound port: read-through cache of `MBEnrichment` keyed by `(kind, mbid)` or negative-cached `(kind, normalized title+subtitle)`. Redis-backed, no-op when absent. Defined in `services/go-api/internal/discovery/ports/`.
- **external_ids** — field on `MBEnrichment`: lowercase provider name → bare id (`deezer`, `spotify`, `discogs`, `wikidata`), from MB url-relations. Display-only in v1, not yet a merge input.

- **EntityIdentity** — durable cross-provider identity record: `(provider, external_id, kind) → (mbid, xref)`. Lets a later MB-absent search resolve identity (and artwork) from what a prior MB-present search learned. Keyed on stable provider ids, never names. See okf: `discovery/identity-artwork`. Persisted in the `entity_identity` table.
- **IdentityStore** — outbound port over `EntityIdentity`: `PersistBridges(kind, mbid, xref)` records what merge learned; `LookupByProviderID(kind, provider, external_id)` reads it back later. Postgres source of truth, Redis read-through on the hot path. Defined in `services/go-api/internal/discovery/ports/ports_artwork.go`.

- **DiscogsEnrichment** — immutable detail-screen enrichment from Discogs for one album (a "master"): `master_id`, `genres`, `styles`, `year`, `credits`, `labels`, `formats`, `country`, `companies`, `community` (have/want/rating). Not persisted. Complements `MBEnrichment` (Discogs owns credits/styles). Defined in `services/go-api/internal/discovery/domain/discogs_enrichment.go`.
- **DiscogsArtistEnrichment** — immutable detail-screen enrichment from Discogs for one artist: `artist_id`, `profile`, `real_name`, `aliases`, `name_variations`, `members`/`groups`, `links`. Not persisted. Defined in `services/go-api/internal/discovery/domain/discogs_enrichment.go`.
- **DiscogsEnricher** — outbound port: album side `ResolveMasterID(artist, album) → LookupAlbum → DiscogsEnrichment`; artist side `ResolveArtistID(name) → LookupArtist → DiscogsArtistEnrichment`. Defined in `services/go-api/internal/discovery/ports/`.

- **LastFmEnrichment** — immutable detail-screen enrichment from Last.fm: `mbid`, `listeners`+`playcount`, weighted `tags`, `bio`, `similar` (artist only), `duration`+`album` (track only). Not persisted. The listening-behavior axis MB/Discogs lack. Defined in `services/go-api/internal/discovery/domain/lastfm_enrichment.go`.
- **LastFmEnricher** — outbound port: `Lookup(kind, artistName, entityTitle) → LastFmEnrichment` via per-kind `*.getInfo` (Last.fm autocorrects names server-side — no separate id-resolution step). Defined in `services/go-api/internal/discovery/ports/`.

- **DeezerLyrics** — immutable Deezer-sourced lyrics for a track: `plain`, `synced_lines` (LRC-style), `writers`, `copyright`. Not persisted. The one metadata axis no other audited provider carries. Track-only. Defined in `services/go-api/internal/discovery/domain/deezer_lyrics.go`.
- **LyricsProvider** — outbound port: `ResolveTrackID(artist, title) → Lookup(trackID) → DeezerLyrics` via Deezer's internal GraphQL. "No lyrics" returns empty value + nil error (negative-cacheable). Defined in `services/go-api/internal/discovery/ports/`.
- **LyricsCache** — outbound port: read-through cache of `DeezerLyrics` keyed by normalized `(artist, title)`. Long TTL positive, short TTL negative. Redis-backed, no-op when absent. Defined in `services/go-api/internal/discovery/ports/`.

- **Queue** — the runtime playback sequence: ordered tracks, current index, shuffle state, repeat mode. Created on play-from-playlist/library/search. Client-managed; server persists a snapshot for resume-on-reopen. Defined in `apps/mobile/src/shared/playback/queueStore.ts`.
- **RepeatMode** — three-state enum: `off` (stop at end), `all` (loop queue), `one` (loop current track). Defined in `apps/mobile/src/shared/playback/types.ts`.

### Future (illustrative — to be added when the spec that introduces them ships)

- **Library** — a user's personal collection. Each user has exactly one library. The library references tracks from the catalog; it does not own them.
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

- **Song** — synonym of `Track`. The legacy `music-manager` used "song" as its primary noun (`songs` table, `Song` class). altune uses `Track` exclusively. Appears only as source-data naming during the legacy-library import — never as a type or column in altune.
