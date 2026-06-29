# Plan — Durable identity & deterministic artwork resolution

Spec: `docs/specs/identity-artwork-resolution/spec.md`. Vertical slices, each build+test-verified.

## Slice 1 — Discography ordering & years (independent, ship first)
- `service/get_artist_content.go`: sort albums by release-date desc **before** the `limit` truncation; add `sortAlbumsByReleaseDateDesc` helper + `albumYear` normalization (derive `extras["year"]` int from `release_date`).
- `adapters/providers/lastfm.go` `GetArtistAlbums`: populate a year/date where the Last.fm response carries one.
- `apps/mobile/.../useArtistContent.ts:218`: drop the `dzValidated` conditional — always trust backend order.
- Tests: `get_artist_content` table test asserting newest-first + year populated.
- Verify: `go test ./internal/discovery/... -run ArtistContent`.

## Slice 2 — entity_identity migration
- `migrations/008_discovery_entity_identity.sql`: `entity_identity(provider, external_id, mbid, xref jsonb, kind, resolved_at)`, PK `(provider, external_id)`. Human-review gate for prod; apply to local dev for tests.

## Slice 3 — IdentityStore port + adapters
- `ports/ports_artwork.go` (or `ports_identity.go`): `IdentityStore` — `PersistBridges(ctx, mbid string, kind, xref map[string]string) error` and `LookupByProviderID(ctx, provider, externalID string) (mbid string, xref map[string]string, ok bool)`.
- `adapters/persistence/entity_identity_repo.go`: pgx adapter (upsert + lookup), compile-time check.
- `adapters/cache/`: Redis read-through wrapper over the Postgres store (nil-safe), or fold into existing enrichment cache.
- Verify: `go build ./...`.

## Slice 4 — write + read wiring + cache-gating (B)
- `service/search.go` `stampIdentities`: after stamping `xref`, call `identityStore.PersistBridges`.
- `service/enrich.go` `enrichOne`: before artwork path, `LookupByProviderID` on `result.Sources[0]`; attach mbid+xref. Keep `mbidIndex` as secondary.
- `resolveArtwork`: return a confidence (`identity|name|fallback`); gate `artworkCache.Set` so name/fallback use short TTL and never overwrite an identity entry (extend `ArtworkCache` or add a confidence param).
- `service/search.go` `Service` struct + `WithIdentityStore` option.

## Slice 5 — composition root
- `internal/app/app.go`: construct the pgx identity repo + Redis read-through, inject via `WithIdentityStore`.

## Slice 6 — verify
- `go build ./... && go vet ./... && go test ./... -count=1`.
- Acceptance (AC1): `discoveryeval -query "Che"` with MB present (populate) → `redis-cli flushall` → re-run; assert artist still carries identity + identity-sourced artwork.
- Update `docs/ubiquitous-language.md` (EntityIdentity, IdentityStore).

## Slice 7 — honest placeholder (C, mobile; follow-up if time)
- Client renders initials/blank avatar when `image_url` empty; ensure backend emits empty rather than a fallback stranger when confidence would be `fallback` and no provider image.
