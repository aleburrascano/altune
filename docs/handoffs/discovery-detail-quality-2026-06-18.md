# Handoff: Discovery Detail Quality — Session 2

**Date:** 2026-06-18 (evening session)
**From:** Pipeline hardening + detail quality implementation
**To:** Next session — fix remaining issues, this needs fresh eyes

## What the user sees RIGHT NOW (on phone)

1. **Wrong profile picture** — TheAudioDB returned artwork for a DIFFERENT artist named "Che" (likely a rock band, not the user's hip-hop artist). The artwork enrichment doesn't validate that the resolved image belongs to the correct artist.
2. **"Atlanta rapper" may be wrong** — MusicBrainz labels this Che as "Atlanta rapper" but the user hasn't confirmed this is accurate. The disambiguation text needs verification.
3. **Discographies still mixed** — the MB cross-reference backend works (confirmed via API: 8 confirmed, 17 unconfirmed), but the frontend may not be picking it up because the mobile app needs to pass the `name` query param. The frontend code was updated (`useArtistContent.ts`, `ArtistDetailBody.tsx`) but the Expo dev server may need a restart.
4. **Artist spam** — searching "Che" returns 7 separate artist results all named "Che" (some with "Atlanta rapper" subtitle, some without). No dedup or grouping strategy for same-name artists.

## What was implemented this session

### Pipeline hardening (20 requirements, all tested — 498/498 pass)

All changes from the audit:
- **R1: Removed artist name-only merge** (dedup.go) — artists only merge on MBID
- **R2: Added regression test** for same-name artist collision
- **R3: MusicBrainz disambiguation field** extracted in adapter
- **R4-R5: Vocabulary quality gate** (pop >= 30) + error logging
- **R6-R7: Rerank doc + PopularityDominance readability**
- **R8-R10: Dead code removed** (ApplyIntentBoost, dedupRelatedGroups, preQueryCorrection)
- **R11-R12: Enum zero-value sentinels** (ResultKindUnknown, ProviderUnknown)
- **R13: Provider per-kind error logging**
- **R14-R16: Levenshtein rune fix, context.Background fix**
- **R17: SetNoisePatterns global removed**
- **R18-R20: Precision, off-by-one, VocabularyKind type**
- **CollapseVersions artist guard** — artists are never collapsed as "versions"

### Artist detail quality (multi-provider validation)

- **AlbumValidator port** (`ports/ports.go`) — `ValidateArtistAlbums` + `ResolveArtistIdentity`
- **MusicBrainz implementation** (`musicbrainz.go`) — resolves MBID, fetches release-groups, cross-references Deezer albums. Splits into confirmed (MB-matched) vs unconfirmed.
- **GetArtistContentService** wired with optional `AlbumValidator` — when `name` query param is passed, cross-references Deezer albums against MB. Confirmed first, unconfirmed last.
- **Handler** accepts `?name=` query param on `/artists/{provider}/{externalId}/albums`
- **Frontend** (`useArtistContent.ts`) passes `artistName` to `getArtistAlbums`. API client (`discovery.ts`) appends `&name=` param.
- **MB timeout increased** to 4s (from 1.5s) with artist-first search order

### Disambiguation in search results

- **`applyArtistDisambiguation`** method on `SearchMusicService` — for artist results without subtitle, queries `AlbumValidator.ResolveArtistIdentity` to get MB disambiguation + MBID
- **`transferArtistDisambiguation`** in FuseAndRank — transfers disambiguation from MB-sourced results to matching non-MB results (before gate filters MB results)
- **Wiring**: `WithIdentityResolver` search option, MB adapter instance shared between detail and search services. `searchSvc` construction moved after MB wiring in `app.go`

### Artwork improvements

- **Track fallback** — when artist has no image after standard enrichment, searches for the artist name as a track query to get album cover art
- **Placeholder detection fixed** — `IsDeezerPlaceholder` now checks for the empty MD5 hash `d41d8cd98f00b204e9800998ecf8427e` (was only checking for `/artist//` double-slash pattern)
- **Cache placeholder bypass** — cached placeholder URLs are treated as negative cache hits, triggering the track fallback

## What is NOT working and needs fixing

### P1: Wrong artwork from TheAudioDB

TheAudioDB returned artwork for a different "Che" artist. The artwork enrichment chain resolves images by searching the provider for the artist name — it has no way to verify the returned image is for the CORRECT "Che". Same problem as the Deezer discography contamination but for images.

**Root cause**: Artwork resolvers (Deezer, Genius, TheAudioDB, iTunes) search by name only. When multiple artists share a name, the first result's image is used regardless of which artist it belongs to.

**Possible fix**: When we have an MBID, use it to resolve artwork from Fanart.tv (which indexes by MBID). The MBID is now available on the search result (`extras.mbid`). Fanart.tv is already in the enrichment chain but only fires when `mbid != ""` — the MBID was previously not set on Deezer results. Now it IS set (via `applyArtistDisambiguation`), but the enrichment runs BEFORE disambiguation. **The fix might be as simple as reordering: run disambiguation before enrichment.**

### P2: Disambiguation accuracy

"Atlanta rapper" comes from MusicBrainz. The user hasn't confirmed this is correct. If the MB entry refers to a different "Che" who happens to be from Atlanta, the disambiguation would be wrong. The `resolveArtistMBID` picks the FIRST exact name match from MB's artist search — it doesn't verify against Deezer's data (fan count, genres, etc.).

**Possible fix**: Cross-reference MB match against Deezer data — if MB artist has release-groups that match Deezer's confirmed albums, it's the right one. If not, try the next MB match.

### P3: Discography still mixed on the phone

The backend correctly separates confirmed vs unconfirmed albums (verified via direct API call). But the user reports the phone still shows mixed discography. Possible causes:
- The Expo dev server may not have picked up the frontend changes (`useArtistContent.ts`, `ArtistDetailBody.tsx`, `discovery.ts`)
- React Query cache may be stale (30min staleTime on artist-albums queries)
- The frontend's own MB-authoritative filter may be conflicting with the backend's validation

**Debug steps**: Check the network tab in the Expo dev tools. Verify the request URL includes `&name=Che`. Check the response order.

### P4: Artist spam in search results

7 "Che" artist results with the same name. The merge fix (R1) correctly keeps them separate, and CollapseVersions no longer collapses them. But the UX is bad — the user sees a wall of identical entries.

**Possible approaches** (not implemented):
- Limit same-name artists to top 2-3 in results
- Group same-name artists into a disambiguation UI ("Did you mean: Che (Atlanta rapper), Che (Korean singer-songwriter), ...?")
- Only show the highest-popularity result per normalized artist name, with a "see more" option

## Key files changed

### Backend (Go)
- `services/go-api/internal/discovery/service/dedup.go` — removed name-only merge, CollapseVersions artist guard, transferArtistDisambiguation, PopularityDominance readability
- `services/go-api/internal/discovery/service/search_music.go` — vocab quality gate, applyArtistDisambiguation (MB identity resolution), artwork placeholder bypass, track fallback
- `services/go-api/internal/discovery/service/get_artist_content.go` — AlbumValidator wiring, artistName param
- `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — ValidateArtistAlbums, ResolveArtistIdentity, fetchReleaseGroups, fetchArtistMatches, disambiguation extraction, SearchTimeout 4s
- `services/go-api/internal/discovery/adapters/providers/deezer.go` — IsDeezerPlaceholder fix (empty hash detection), per-kind error logging
- `services/go-api/internal/discovery/adapters/handler/discovery_handler.go` — artistName query param
- `services/go-api/internal/discovery/ports/ports.go` — AlbumValidator, ArtistIdentity, AlbumValidationResult
- `services/go-api/internal/discovery/domain/types.go` — ResultKindUnknown, ProviderUnknown sentinels
- `services/go-api/internal/discovery/domain/vocabulary.go` — VocabularyKind type
- `services/go-api/internal/app/app.go` — MB validator wiring for both detail and search services

### Frontend (TypeScript)
- `apps/mobile/src/shared/api-client/discovery.ts` — `getArtistAlbums` accepts `artistName` param
- `apps/mobile/src/features/detail/hooks/useArtistContent.ts` — passes `artistName` to Deezer album query
- `apps/mobile/src/features/detail/ui/ArtistDetailBody.tsx` — passes `result.title` as `artistName`

### Docs
- `docs/brainstorms/discovery-pipeline-hardening-requirements.md` — R1-R20 audit requirements
- `docs/brainstorms/artist-detail-quality-requirements.md` — detail quality spec
- `docs/handoffs/discovery-pipeline-hardening-2026-06-18.md` — first handoff (updated)

## How to verify

```bash
cd services/go-api

# Tests (should be 498 passing)
go test ./internal/discovery/... -count=1 -timeout=120s

# Build
go build -o ./tmp/api.exe ./cmd/api

# Clear stale artwork cache before testing
docker exec altune-redis-dev redis-cli EVAL "local keys = redis.call('keys', 'discovery:artwork:v1:artist:*'); for i,k in ipairs(keys) do redis.call('del', k) end; return #keys" 0

# Run with debug logging
LOG_LEVEL=debug ./tmp/api.exe 2>&1 | tee tmp/debug.log

# Test search
curl "http://192.168.2.10:8000/v1/discovery/search?q=Che&limit=10" -H "Authorization: Bearer <token>"

# Test album validation (should show confirmed albums first)
curl "http://192.168.2.10:8000/v1/discovery/artists/deezer/234701081/albums?limit=25&name=Che" -H "Authorization: Bearer <token>"
```

## Recommended next session approach

1. **Start with the four P-issues above** — P1 (wrong artwork), P2 (disambiguation accuracy), P3 (frontend not picking up changes), P4 (artist spam)
2. **Brainstorm P4 first** — artist spam affects every ambiguous search. Need a UX decision: limit, group, or collapse same-name artists
3. **P1 fix might be simple** — reorder: run disambiguation (which sets MBID) BEFORE enrichment (which uses MBID for Fanart.tv). Test with "Che"
4. **P3 might just need an Expo restart** — verify the frontend is actually sending `&name=Che` in the network request
5. **Test with multiple artists** — "Aurora", "Banks", "Common" — to confirm fixes are generic

## Wikipedia database list (from user)

https://en.wikipedia.org/wiki/List_of_online_music_databases

Candidates for future provider integration: Discogs, AllMusic, Rate Your Music
