# musicbrainz-enrichment — implementation plan

Spec: docs/specs/musicbrainz-enrichment/spec.md

Pattern source: mirrors the **related-tracks** detail-open wiring end-to-end (adapter method →
service → handler route → api-client → hook → UI), and the **artwork-cache** read-through pattern
(`RedisArtworkCache`). The MB lookup methods are new surface on the existing `MusicBrainzAdapter`
(`docs/providers/musicbrainz.md` §4, all endpoints live-probed 2026-06-22).

Vault: [vault: wiki/concepts/Lazy Initialization.md] (load-on-first-access + memoize),
[vault: wiki/concepts/Proxy Pattern.md] (caching proxy). Read-only, off ranking path; no pattern
stretched, no eval gate.

## Slices

### Slice 1: domain `MBEnrichment` + ports `MetadataEnricher`, `EnrichmentCache`
- Acceptance criterion: DTO invariants (foundation for all ACs)
- Files:
  - `services/go-api/internal/discovery/domain/enrichment.go` (new) — `MBEnrichment` immutable struct:
    `MBID, Genres []string, Year int, Rating float64, RatingVotes int, PrimaryType string,
    SecondaryTypes []string, ExternalIDs map[string]string, ArtworkURL string`; `EmptyEnrichment()`
    constructor returning a value with non-nil `Genres`/`SecondaryTypes`/`ExternalIDs`; `IsZero()`.
  - `services/go-api/internal/discovery/ports/ports.go` — add `MetadataEnricher` (`ResolveMBID(ctx,
    kind, title, subtitle) (string, error)`; `Lookup(ctx, kind, mbid) (domain.MBEnrichment, error)`)
    and `EnrichmentCache` (`Get(ctx, kind, mbid) (domain.MBEnrichment, bool, error)`;
    `Set(ctx, kind, mbid string, e domain.MBEnrichment) error`; `GetNegative`/`SetNegative(ctx, kind,
    nameKey)` for the unresolved path).
  - `services/go-api/internal/discovery/domain/enrichment_test.go` (new).
- Domain (VO) + application (ports). No adapter yet.
- Failing test first: `TestEmptyEnrichment_NonNilCollections` — `EmptyEnrichment()` has non-nil empty
  slices/map and `IsZero()` true.
- Verify: `cd services/go-api && go test ./internal/discovery/domain/ -run Enrichment -count=1`

### Slice 2a: MB adapter `Lookup` — artist path + mapping helpers
- Acceptance criterion: AC#1
- Files:
  - `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — add `Lookup(ctx, kind,
    mbid)` with the **artist** branch: GET `/artist/{mbid}?inc=genres+ratings+url-rels&fmt=json`; map
    `genres[]` via a `sortedGenres` helper (dedup, vote-count desc, **alpha tie**), `rating.value`+
    `votes-count`, and `relations[]`→`ExternalIDs` via an `externalIDsFromRelations` helper (relation
    `type` in {discogs, wikidata}; `free streaming` host→deezer/spotify; value = last non-empty path
    segment; keys lowercase). New structs `mbLookupArtist`, `mbGenre`, `mbRating`, `mbRelation`.
    Artist DTO has `year:0`/`primary_type:""`/`secondary_types:[]`.
  - `services/go-api/internal/discovery/adapters/providers/musicbrainz_enrichment_test.go` (new) —
    fixture captured from the live probe (Kendrick artist).
- Adapter (outbound). Reuses `rateLimit()`, `userAgent`, `parseBirthYear`.
- Failing test first: `TestMusicBrainzAdapter_Lookup_Artist` — genres sorted vote-desc with alpha
  tie + deduped; rating mapped; `external_ids` has bare deezer/spotify/discogs/wikidata ids (URL
  stripped to last segment); irrelevant relation types ignored.
- Verify: `cd services/go-api && go test ./internal/discovery/adapters/providers/ -run Lookup_Artist -count=1`

### Slice 2b: MB adapter `Lookup` — release-group path
- Acceptance criterion: AC#2
- Files:
  - `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — add the **release-group**
    branch of `Lookup`: GET `/release-group/{mbid}?inc=genres+ratings&fmt=json`; reuse `sortedGenres`;
    map `first-release-date`→`Year` (4-digit prefix via `parseBirthYear`; `0` on malformed),
    `primary-type`, `secondary-types[]`. New struct `mbLookupReleaseGroup`. A non-200 (incl. a passed
    mbid that 404s) returns an error (service degrades it — AC#6).
  - extend `musicbrainz_enrichment_test.go` (DAMN. release-group fixture).
- Adapter (outbound).
- Failing test first: `TestMusicBrainzAdapter_Lookup_ReleaseGroup` — genres mapped, `year:2017`,
  `primary_type:"Album"`, secondary_types `[]` when none; **404 on the lookup → error**.
- Verify: `cd services/go-api && go test ./internal/discovery/adapters/providers/ -run Lookup_ReleaseGroup -count=1`

### Slice 3: MB adapter `ResolveMBID` (strict name match)
- Acceptance criterion: AC#3 (resolution predicate)
- Files:
  - `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — add `ResolveMBID(ctx,
    kind, title, subtitle)`: search the kind's entity, scan candidates and return the **first** whose
    `textnorm.NormalizeForMatch(title)` matches **and** (subtitle non-empty, non-artist kind) whose
    normalized primary artist-credit matches subtitle; else `""`. Reuse `searchKind` response structs.
    Add `var _ ports.MetadataEnricher = (*MusicBrainzAdapter)(nil)` compile check here (interface now
    fully implemented).
  - extend `musicbrainz_enrichment_test.go`.
- Adapter (outbound).
- Failing test first: `TestMusicBrainzAdapter_ResolveMBID` table — exact title+artist → mbid; title
  match but artist mismatch → ""; **first candidate is a near-miss + second is exact → returns the
  second** (predicate filters, doesn't blindly take result[0]); artist-kind title-only → mbid;
  no match → "".
- Verify: `cd services/go-api && go test ./internal/discovery/adapters/providers/ -run ResolveMBID -count=1`

### Slice 4: `EnrichmentService` (orchestration + cache + degradation)
- Acceptance criterion: AC#4, AC#5, AC#6
- Files:
  - `services/go-api/internal/discovery/service/enrichment.go` (new) — `EnrichmentService{ enricher
    ports.MetadataEnricher; artwork ports.ArtworkResolver; cache ports.EnrichmentCache }`,
    `Execute(ctx, kind, title, subtitle, mbidParam) (domain.MBEnrichment, error)`: (1) determine mbid
    — passed wins; else negative-cache check then `ResolveMBID`, on "" set negative cache + return
    `EmptyEnrichment`; (2) positive cache check → return whole value; (3) `Lookup` (on error →
    `EmptyEnrichment`, best-effort, **don't** poison positive cache); (4) `artwork.Resolve(kind,
    title, subtitle, mbid)` → `ArtworkURL`; (5) `cache.Set` whole value; return. nil cache/artwork
    deps are tolerated (no-op).
  - `services/go-api/internal/discovery/service/enrichment_test.go` (new) — in-memory call-counting
    fakes for enricher/artwork/cache.
- Application layer (use case).
- Failing test first: `TestEnrichmentService_Execute` table — (a) passed mbid → lookup+artwork merged,
  cached; (b) second identical call → 0 extra enricher AND 0 extra artwork calls (AC#5); (c) lookup
  errors → empty enrichment, 200-equivalent nil error (AC#6); (c2) **passed mbid whose lookup 404s →
  empty enrichment, nil error** (AC#6 explicit 404 path); (d) artwork resolver returns URL → present
  on result (AC#4); (e) ResolveMBID "" → empty + negative-cached (no repeat resolve).
- Verify: `cd services/go-api && go test ./internal/discovery/service/ -run Enrichment -count=1`

### Slice 5: `RedisEnrichmentCache` adapter
- Acceptance criterion: AC#5 (cache mechanics), key shape
- Files:
  - `services/go-api/internal/discovery/adapters/cache/enrichment_cache.go` (new) — mirror
    `RedisArtworkCache`: JSON-marshal `MBEnrichment`; positive key
    `discovery:mbenrich:v1:{kind}:{mbid}` (14-day TTL); negative key
    `discovery:mbenrich:neg:v1:{kind}:{sha256(norm(title+" "+subtitle))}` (24-hour TTL, empty value);
    nil client → no-op (Get miss, Set nil). Implements `ports.EnrichmentCache`.
  - `services/go-api/internal/discovery/adapters/cache/enrichment_cache_test.go` (new) — nil-client
    no-op + key determinism (no live Redis; mirrors how artwork/query cache unit-test key logic).
- Adapter (outbound).
- Failing test first: `TestRedisEnrichmentCache_NilClientNoOp` + `TestEnrichmentCacheKey_Deterministic`.
- Verify: `cd services/go-api && go test ./internal/discovery/adapters/cache/ -run Enrichment -count=1`

### Slice 6: HTTP route + DTO + DI wiring
- Acceptance criterion: AC#7, end-to-end shape
- Files:
  - `services/go-api/internal/discovery/adapters/handler/discovery_handler.go` — add `enrichSvc
    *service.EnrichmentService` to struct + `NewDiscoveryHandler` params; route
    `r.Get("/enrichment", h.handleEnrichment)`; `handleEnrichment`: parse+validate `kind`
    (`domain.ParseResultKind`, 400 on missing/unknown), `title`/`subtitle`/`mbid` query params (400 if
    title blank AND mbid blank), nil-svc guard → 200 empty DTO, call `Execute`, map to
    `EnrichmentResponseDTO` (honoring DTO invariants — never-null collections).
  - `services/go-api/internal/app/app.go` — construct `EnrichmentService` (reuse `sharedMB` as the
    `MetadataEnricher`, `buildArtworkChain(cfg)` as the resolver, `NewRedisEnrichmentCache(redis)`),
    pass into `NewDiscoveryHandler` (update the call site + any test constructors).
  - `services/go-api/internal/discovery/adapters/handler/discovery_handler_enrichment_test.go` (new).
- Adapter (inbound) + composition root.
- Failing test first: `TestDiscoveryHandler_Enrichment` — `kind=album&title=DAMN.&subtitle=Kendrick
  Lamar` → 200 DTO with genres/year; missing `kind` → 400; unknown `kind` → 400; blank title + no
  mbid → 400; nil service → 200 empty.
- Verify: `cd services/go-api && go test ./internal/discovery/... -count=1 && go build -o ./tmp/api.exe ./cmd/api`

### Slice 7: mobile api-client `getEnrichment` + `useEnrichment` hook
- Acceptance criterion: AC#8 (data path + gating)
- Files:
  - `apps/mobile/src/shared/api-client/discovery.ts` — add `getEnrichment({kind, title, subtitle?,
    mbid?})` → `EnrichmentResponse` hitting `/v1/discovery/enrichment?...`; add the `EnrichmentResponse`
    type.
  - `apps/mobile/src/features/detail/hooks/useEnrichment.ts` (new) — `useQuery` keyed
    `['enrichment', kind, mbid ?? title+subtitle]`, `enabled` when a title (or mbid) exists, 24h
    staleTime; returns `{ enrichment, isLoading, isError }`; treats empty enrichment (`mbid===""` and
    no genres) as "nothing to show."
  - `apps/mobile/src/features/detail/__tests__/useEnrichment.test.ts` (new).
- Mobile slice (feature folder).
- Failing test first: `useEnrichment` — (a) returns mapped enrichment; (b) empty payload → treated as
  no-data; (c) error payload → `isError`, no throw.
- Verify: `cd apps/mobile && npx jest useEnrichment && npx tsc --noEmit`

### Slice 8: mobile `EnrichmentSection` + wire into detail body + artwork upgrade
- Acceptance criterion: AC#8 (render + hide + artwork upgrade)
- Files:
  - `apps/mobile/src/features/detail/ui/EnrichmentSection.tsx` (new) — renders genre chips (top 4),
    `year`, and `rating` (when > 0); renders `null` when loading-with-no-data / empty / error
    (AC#8 hide). testIDs `detail-enrichment`, `detail-genre-<n>`.
  - the detail body component (`TrackDetailBody.tsx` and the album/artist detail bodies as applicable)
    — render `<EnrichmentSection .../>`; pass `enrichment.artworkUrl` to the existing artwork element
    so a non-empty value upgrades the displayed cover (fall back to the result's own `image_url`).
  - `apps/mobile/src/features/detail/__tests__/EnrichmentSection.test.tsx` (new).
- Mobile slice.
- Failing test first: `EnrichmentSection` — (a) genres present → `detail-enrichment` + N chips;
  (b) empty → renders nothing; (c) artwork_url present → cover uses it.
- Verify: `cd apps/mobile && npx jest EnrichmentSection detail && npx tsc --noEmit`

## Final verification (whole feature)
- `cd services/go-api && go test ./internal/... -count=1 && go vet ./internal/discovery/... && go build -o ./tmp/api.exe ./cmd/api`
- `cd apps/mobile && npx jest detail && npx tsc --noEmit`
- agent-browser smoke (per `ui-testing-workflow.md`): open a detail screen, confirm the metadata block
  renders genres/year and the cover sharpens. **Caveat:** auth + expo-web limits (per the rule) may
  block authed detail screens on web; fall back to a device/unit-test note if the web target can't
  reach an enriched detail. No live MB/eval run is possible in this dev env (1 req/sec, no creds) — the
  adapter is covered by httptest fixtures from the live probe.

## Risks
- **MB 1 req/sec wall** — detail-open placement + read-through cache + best-effort degradation (spec
  Risks; go-resilience: the MB adapter already enforces the limit and the call has a timeout).
- **Wrong-entity name resolution** — strict normalized title(+artist) equality; empty over guess
  (AC#3). Blast radius is one detail screen, never search ordering.
- **Enrichment endpoint bypasses the per-provider circuit breaker** (breaker lives in the search
  fan-out) — consistent with the existing artist/album/related content endpoints; the MB client's
  timeout + the cache bound the exposure.
- **`NewDiscoveryHandler` signature change** ripples to every constructor call site (app.go + any
  handler tests) — update them in Slice 6; `go build` is the gate.
- **Stale/invalid passed `mbid`** (404) — falls under AC#6 graceful path (empty enrichment).

## ADR candidates
- None. No new external dependency (MusicBrainz + Cover Art Archive already integrated), no new
  aggregate, no cross-context coupling; `MetadataEnricher`/`EnrichmentCache` are siblings of the
  existing content/artwork ports. external_ids are returned but deliberately **not** fed into
  `Merge`/`Rank` (that crossing would need an ADR + the eval gate — explicitly deferred in the spec).
