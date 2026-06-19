# Handoff: Artist Identity v2 — Session 2026-06-19

**Date:** 2026-06-19 (overnight session)
**Branch:** `feat/artist-identity-v2` (4 commits, clean git status)
**From:** Backend audit + artist identity v2 implementation
**To:** Next session — test on phone, fix remaining contamination, frontend P4

## What the user sees RIGHT NOW

1. **Artwork fixed** — Discogs image (`i.discogs.com/...`) shows for "Che" instead of TheAudioDB's wrong stoner rock band image. Confirmed via `/test-search`.
2. **Artist collapse working** — searching "Che" shows 1 artist result with `collapsed=6` other same-name artists grouped inside `extras.collapsed_artists`.
3. **Disambiguation working** — "Atlanta rapper" subtitle, correct MBID `0a68f3b5...`.
4. **71 albums showing** (up from 25) — Deezer album limit increased from 25 to 100. "agenda" (2021) and all other singles now visible.
5. **MB-confirmed albums sort first** — REST IN BASS, Sayso Says, closed captions at top. Contamination (Samsonite, Gallos Ciegos, etc.) follows after.
6. **Contamination NOT yet removed** — Discogs resolved to the wrong "Che" (overlap=0, too niche for Discogs). With only MB as one signal, the 2-signal filter threshold isn't met. Contamination is deprioritized but still visible.
7. **Frontend discography ordering** — backend returns correct order. Frontend `useArtistContent` was updated to trust backend ordering when `artistName` is provided (no client-side re-sort). User restarted Expo 3 times but hasn't confirmed this is working on phone yet.

## What was implemented this session

### Backend audit fixes
- H1: Type-safe intent comparison (`domain.VocabKindArtist`)
- H2: Tracked vocabulary ingestion goroutine (`ingestWg` + `WaitForIngest()`)
- M1-M2: `ParseProviderName` errors handled in content services
- M3: Removed nil receiver guard in `FindRelatedService`
- M4: CORS ngrok header dev-only
- M5: Skipped (Execute() refactor — too risky for this session)
- L1-L3,L5: Magic number documentation
- L6: Content endpoint limit caps (100/50/100)

### Pipeline improvements
- `CollapseArtistDuplicates` — groups same-name artists, keeps highest-pop as primary
- Pipeline reorder: `CollapseArtistDuplicates` → `applyArtistDisambiguation` → `enrich` → `Rerank`
- Per-name identity cache in `applyArtistDisambiguation` (no duplicate MB calls)
- Shared single `MusicBrainzAdapter` instance with mutex-based 1 req/sec rate limiter
- Pipeline summary log (`search.complete` with all signals in one line)

### Artist identity v2 — new providers
- **Discogs adapter** (`discogs.go`): artwork resolver + artist identity resolution via album-title overlap + rate limiter
- **YouTube adapter** (`youtube.go`): channel thumbnail artwork resolver with disambiguation query
- **Discogs ID cache** (`discogs_cache.go`): Redis, 30-day TTL
- **AudioFingerprinter port** (`ports.go`): interface only, no implementation
- Artwork chain reordered: Fanart.tv → Discogs → YouTube → Genius → Deezer → TheAudioDB → iTunes
- Deezer album fetch limit: 25 → 100
- `ProviderDiscogs` and `ProviderYouTube` enum values added

### Discography quality
- `FilterContamination` with separate `MBConfirmed` / `DiscogsConfirmed` sets
- When Discogs overlap=0, its confirmed set is NOT used (unreliable match)
- Birth year extracted from MB `life-span.begin` for year-floor heuristic (birth_year + 14)
- Discogs year backfill on album extras when overlap > 0

### Frontend
- Removed dead MB album query from `useArtistContent` (MB isn't a content provider)
- `dzValidated` flag: trust backend ordering when `artistName` is provided
- Removed `mbid` prop from `useArtistContent` interface
- Updated all 3 test files (authority, status, main)

## What is NOT working and needs fixing

### P-priority: Discography contamination still visible

**Root cause:** Che is too niche for Discogs (overlap=0, wrong artist resolved). With only MB as one signal source, unconfirmed albums get 1 mismatch signal — not enough for the 2-signal removal threshold. This is genuinely hard (Spotify's own ML paper reports 45% precision).

**Possible next steps:**
- Add more cross-reference sources (YouTube Music metadata? Genius discography?)
- Use track-level metadata heuristics (featured artists, producer names — if the featured artists on an album don't overlap with the confirmed albums' featured artists, it's likely contamination)
- AcoustID fingerprinting when audio is downloaded (long-term, needs acquisition pipeline)
- Accept partial contamination for niche artists and focus on getting the confirmed albums to the top (current behavior)

### P-priority: Frontend verification needed

The frontend changes (trust backend ordering, removed MB query) haven't been verified on the actual phone. The user restarted Expo 3 times but didn't confirm results. Need to:
1. Restart Expo dev server (`npx expo start`)
2. Search "Che" → tap into artist → verify discography shows confirmed first
3. Verify the Discogs artwork shows correctly on the artist card

### P4: Frontend collapsed_artists rendering

`CollapseArtistDuplicates` populates `extras.collapsed_artists` on the primary artist result. The frontend doesn't render this yet — it just shows the primary artist. Need a tappable "N other artists named X" expansion row in search results. The data is already on the wire.

### LOTTO DREAMS is contamination

The user confirmed that "LOTTO DREAMS" does NOT belong to Che. It's currently showing as the first album because it's the newest (2026-06-13) and isn't filtered. This is contamination that happens to be recent — the year-floor heuristic doesn't catch it.

## Key files changed

### New files
- `services/go-api/internal/discovery/adapters/providers/discogs.go`
- `services/go-api/internal/discovery/adapters/providers/youtube.go`
- `services/go-api/internal/discovery/adapters/cache/discogs_cache.go`
- `services/go-api/internal/discovery/service/discography_filter.go`
- `services/go-api/internal/discovery/service/discography_filter_test.go`

### Key modified files
- `services/go-api/internal/app/app.go` — shared MB adapter, Discogs/YouTube wiring, artwork chain reorder
- `services/go-api/internal/discovery/service/search_music.go` — pipeline reorder, collapse, disambiguation cache, summary log
- `services/go-api/internal/discovery/service/dedup.go` — `CollapseArtistDuplicates`
- `services/go-api/internal/discovery/service/get_artist_content.go` — Discogs enrichment, `FilterContamination`
- `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — birth year extraction, rate limiter, simplified `ResolveArtistIdentity`
- `services/go-api/internal/discovery/ports/ports.go` — `AudioFingerprinter`, `DiscographyEnricher`, `DiscogsArtistInfo`, `BirthYear`
- `apps/mobile/src/features/detail/hooks/useArtistContent.ts` — removed MB query, trust backend ordering

## How to verify

```bash
cd services/go-api

# Build
go build -o ./tmp/api.exe ./cmd/api

# Tests (should be 515+ discovery, 920+ total)
go test ./internal/discovery/... -count=1 -timeout=120s
go test ./... -count=1 -timeout=180s

# Clear caches before testing
docker exec altune-redis-dev redis-cli EVAL "local keys = redis.call('keys', 'discovery:artwork:v1:*'); for i,k in ipairs(keys) do redis.call('del', k) end; return #keys" 0

# Run server (needs DISCOGS_TOKEN and YOUTUBE_API_KEY in .env)
./tmp/api.exe

# Test search
curl "http://localhost:8000/v1/discovery/search?q=Che&limit=10" -H "Authorization: Bearer <token>"

# Test discography
curl "http://localhost:8000/v1/discovery/artists/deezer/234701081/albums?limit=100&name=Che" -H "Authorization: Bearer <token>"
```

## Recommended next session approach

1. **Verify on phone first** — restart Expo, search "Che", check artwork + discography ordering
2. **Frontend P4** — render `collapsed_artists` expansion row in search results
3. **Contamination strategy decision** — brainstorm whether to add more metadata heuristics, accept partial contamination for niche artists, or defer to fingerprinting. The user explicitly said "don't add band-aid solutions for one artist"
4. **Run `/test-search` with diverse queries** — "Aurora", "Banks", "Common" to test genre diversity (the system serves multiple users with different music tastes)

## Docs written this session

- `docs/brainstorms/2026-06-18-discovery-detail-residuals.md` — P1-P4 requirements
- `docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md` — full requirements for Discogs/YouTube integration
- `docs/brainstorms/artist-detail-quality-requirements.md` — original detail quality brainstorm
- `docs/brainstorms/discovery-pipeline-hardening-requirements.md` — R1-R20 audit
- `docs/plans/2026-06-19-001-feat-artist-identity-v2-plan.md` — implementation plan (U1-U9, all completed)
- `docs/handoffs/discovery-detail-quality-2026-06-18.md` — prior session handoff

## Memories saved

- `ml-audio-approach.md` — ML-based audio similarity (Spotify "Which Witch") is deferred, not rejected. Revisit when team grows.
- `multi-user-music-diversity.md` — app serves family/friends with diverse tastes; discovery must be genre-agnostic.
