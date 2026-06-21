# related-tracks — implementation plan

Spec: docs/specs/related-tracks/spec.md

Pattern source: this feature mirrors the **artist-content** wiring end-to-end
(`GetArtistContentService` / `ArtistContentProvider` / `handleArtistTopTracks` /
`getArtistTopTracks` / `useArtistContent`). The only structural difference: it is
**track-keyed** (a new small port `RelatedTracksProvider`) and the rail is gated to
SoundCloud-sourced track results.

Vault: `vk_search "recommendation related content read path"` returned only
tangential matches (no strong pattern). Read-only enrichment; no pattern stretched.

## Slices

### Slice 1: SoundCloud adapter `GetRelatedTracks` + `RelatedTracksProvider` port
- Acceptance criterion: AC#1, AC#2
- Files:
  - `services/go-api/internal/discovery/ports/ports.go` — add `RelatedTracksProvider` interface (one method, track-keyed).
  - `services/go-api/internal/discovery/adapters/providers/soundcloud_apiv2.go` — add `scRelatedLimit = 20` const, `GetRelatedTracks(ctx, _, externalID)` calling `/tracks/{id}/related?client_id=&limit=`, reuse `mapSoundCloudAPITrack`; add `var _ ports.RelatedTracksProvider = (*SoundCloudAPIAdapter)(nil)`.
  - `services/go-api/internal/discovery/adapters/providers/soundcloud_apiv2_related_test.go` (new).
- Domain/application/adapter: new outbound port (application layer) + its SC implementation (adapter). Reuses `scSearchResponse` + `mapSoundCloudAPITrack`.
- Failing test first: `TestSoundCloudAPIAdapter_GetRelatedTracks_MapsCollection` — httptest server returns a `{collection:[…]}` body for `/tracks/123/related`; assert N domain results, `kind=track`, SC `SourceRef` with numeric id, genre/playback extras carried; one unmappable item dropped.
- Verify: `cd services/go-api && go test ./internal/discovery/adapters/providers/ -run GetRelatedTracks -count=1`

### Slice 2: `GetRelatedTracksService` use case
- Acceptance criterion: AC#3 (unsupported provider), limit slicing
- Files:
  - `services/go-api/internal/discovery/service/get_related_tracks.go` (new) — `GetRelatedTracksService{ providers map[string]ports.RelatedTracksProvider }`, `Execute(ctx, providerName, externalID, limit) (*ContentFetchResponse, error)`. Mirror `GetArtistContentService.GetTopTracks` exactly: unknown provider / parse error / provider error → `ProviderStatusError` + empty `Items` (200, never error the request); ok → slice to `limit`.
  - `services/go-api/internal/discovery/service/get_related_tracks_test.go` (new).
- Application layer: new use case; reuses existing `ContentFetchResponse`.
- Failing test first: `TestGetRelatedTracksService_Execute` table — `"unknown provider"` → status error + empty; `"provider returns error"` → status error + empty; `"ok slices to limit"` → status ok, len==limit. Uses an in-memory fake `RelatedTracksProvider`.
- Verify: `cd services/go-api && go test ./internal/discovery/service/ -run GetRelatedTracks -count=1`

### Slice 3: HTTP route + DI wiring
- Acceptance criterion: AC#3 (200 on unsupported), route shape
- Files:
  - `services/go-api/internal/discovery/adapters/handler/discovery_handler.go` — add `relatedSvc *service.GetRelatedTracksService` to struct + `NewDiscoveryHandler` params; route `r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)`; `handleRelatedTracks` mirrors `handleArtistTopTracks` (validateContentParams, limit default 20 / cap 50, nil-svc guard → 200 empty error, call `Execute`, `contentFetchToDTO`).
  - `services/go-api/internal/app/app.go` — build `map[string]ports.RelatedTracksProvider{ "soundcloud": scAdapter }`, construct `GetRelatedTracksService`, pass into `NewDiscoveryHandler` (update the call site + any other constructor callers).
  - `services/go-api/internal/discovery/adapters/handler/discovery_handler_related_test.go` (new).
- Adapter (inbound) + composition root.
- Failing test first: `TestDiscoveryHandler_RelatedTracks` — httptest against a handler with a fake service: `GET /tracks/soundcloud/123/related` → 200 with items; `GET /tracks/deezer/9/related` → 200 status `error`, empty items; missing externalId → 400.
- Verify: `cd services/go-api && go test ./internal/discovery/... -count=1 && go build -o ./tmp/api.exe ./cmd/api`

### Slice 4: typed api-client `getRelatedTracks`
- Acceptance criterion: AC#5 (data path)
- Files:
  - `apps/mobile/src/shared/api-client/discovery.ts` — add `getRelatedTracks(provider, externalId, limit?)` returning `ContentFetchResponse`, hitting `/v1/discovery/tracks/{provider}/{externalId}/related` (reuse `_contentUrl`).
- Failing test first: covered by the hook test (Slice 5) — no separate api-client test file exists for the sibling functions; matching that convention.
- Verify: `cd apps/mobile && npx tsc --noEmit` (typecheck the new export)

### Slice 5: `useRelatedTracks` hook (SC-gated)
- Acceptance criterion: AC#4 (gating), AC#5, AC#7 (graceful)
- Files:
  - `apps/mobile/src/features/detail/hooks/useRelatedTracks.ts` (new) — accepts `{ sources, enabled }`; picks the `soundcloud` source; `useQuery` keyed `['related-tracks', scExternalId]`, `enabled: enabled && scSource !== null`, 30-min staleTime; returns `{ relatedTracks, isLoading, isError }` (items only when `status==='ok'`, `[]` otherwise — non-ok payload treated as failure, AC#7).
  - `apps/mobile/src/features/detail/__tests__/useRelatedTracks.test.ts` (new).
- Failing test first: `useRelatedTracks` — (a) SC source present → returns mapped items; (b) no SC source → query disabled, `relatedTracks` empty, no fetch; (c) status `error` payload → `isError` true, empty items.
- Verify: `cd apps/mobile && npx jest useRelatedTracks`

### Slice 6: `RelatedTracksSection` + wire into `TrackDetailBody`
- Acceptance criterion: AC#5, AC#6 (empty hidden), AC#8 (lateral nav)
- Files:
  - `apps/mobile/src/features/detail/ui/RelatedTracksSection.tsx` (new) — horizontal scroll row of related-track cards; renders `null` when no SC source / loading-with-no-data / empty / error (AC#6, AC#7). Cards tap → navigate to that track's detail reusing the existing **content-item navigation** the album tracklist / top-track rows already use (handoff push, not a re-search). testIDs: `detail-related` (container), `detail-related-<n>` (each card).
  - `apps/mobile/src/features/detail/ui/TrackDetailBody.tsx` — render `<RelatedTracksSection result={result} … />` below the Save button.
  - `apps/mobile/src/features/detail/__tests__/RelatedTracksSection.test.tsx` (new); extend `DetailScreen.test.tsx` only if a wiring assertion is needed.
- Failing test first: `RelatedTracksSection` — (a) SC-sourced track with items → renders `detail-related` + N `detail-related-<n>` cards; (b) non-SC result → renders nothing; (c) empty items → renders nothing; (d) tapping a card triggers navigation to that track.
- Verify: `cd apps/mobile && npx jest RelatedTracksSection DetailScreen`

## Final verification (whole feature)
- `cd services/go-api && go test ./internal/discovery/... -count=1 && go vet ./internal/discovery/... && go build -o ./tmp/api.exe ./cmd/api`
- `cd apps/mobile && npx jest detail && npx tsc --noEmit`
- agent-browser smoke (per `ui-testing-workflow.md`): open a track detail for a SoundCloud-sourced result, confirm the "Related on SoundCloud" rail renders and a card tap navigates. (Auth/web caveats apply; fall back to device note if the web target can't reach an SC-sourced detail.)

## Risks
- **SC `client_id` rotation** breaks the endpoint mid-session — the self-healing resolver (`resolveAndFetch`) re-resolves on 401/403; AC#7 hides the rail if it stays down. (spec Risks)
- **Rate limits** — one bounded call per detail open, cached by query key; no prefetch. (spec Risks; go-resilience: timeout already enforced in `getJSON`/client.)
- **Content endpoints bypass the per-provider circuit breaker** (the breaker lives in the search fan-out, not the artist/album/related content path). Consistent with the existing artist-content/album-tracks endpoints — acceptable; the SC client's own timeout bounds the call.
- **Raw SC ordering quality** (spec open question) — ship raw; spot-check a few seeds; add seed-track drop / dedup only if visibly poor.
- **Multi-SC-source result** (spec open question) — take the first `soundcloud` source's id.

## ADR candidates
- None. No new external dependency, no new aggregate, no cross-context coupling; the new `RelatedTracksProvider` port is a sibling of the existing content ports.
```
