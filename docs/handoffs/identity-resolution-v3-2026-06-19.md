# Handoff: Identity Resolution v3 — Session 2026-06-19

**Date:** 2026-06-19
**Branch:** `feat/artist-identity-v2` (14 commits this session)
**From:** Audit → brainstorm → grill → plan → implement → live debug
**To:** Next session — fix remaining bugs, optimize cold-cache speed, verify on phone

## What the user sees RIGHT NOW

Search "Che" → tap artist → discography loads in ~38 seconds (cold cache). Shows:
- **28 confirmed** albums (REST IN BASS, Sayso Says, closed captions, Fully Loaded, etc.)
- **9 unknown** albums (mix of real Che albums and suspect contamination)
- **44 removed** (LOTTO DREAMS, Samsonite, Kiss Me in the Sky, Gallos Ciegos, Tšernobõl, and 39 others)

Down from 81 total. Major improvement but not done.

## What was implemented this session

### Phase 1: Audit (12 findings, all fixed)
- H1: NormalizeForMatch extracted to `services/go-api/internal/shared/textnorm/`
- H2: io.LimitReader on Discogs/YouTube adapters
- H3: Rate limiter sleep-under-mutex fix
- M1-M7, L1-L2: See commit `7895dc2`

### Phase 2: Brainstorm + Grill
- Requirements doc: `docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md`
- API probing validated: MB has no data on contamination albums, iTunes catches LOTTO DREAMS/Tšernobõl, ISRC registrants differ between real Che and contamination
- Grilling uncovered: MB coverage hole, ISRC registrant signal, distributor-change 24h safeguard, bidirectional R3

### Phase 3: Identity Resolution v3 (8 implementation units)
- U1: Provider metadata extraction (genre_id, tags, area, type, genre)
- U2: ArtistIdentityProfile domain type + AlbumVerdict enum + IdentityResolver port
- U3: Identity resolution Redis cache (30d/24h split TTLs, suspect-state tracking)
- U4: MB reverse-lookup (LookupAlbumArtist)
- U5: iTunes cross-provider search (LookupAlbum, bidirectional)
- U6: Profile constraints (temporal, genre, type) + ISRC registrant fingerprint
- U7: IdentityResolver orchestrator (replaces FilterContamination, deleted discography_filter.go)
- U8: App wiring

### Bugfixes during live testing
- ValidateArtistAlbums returned all-confirmed on MB 503 → fixed to return error
- iTunes title matching missed " - Single" suffix → fixed with stripITunesTypeSuffix
- BuildProfile didn't populate MBConfirmedTitles when ISRC fetcher absent → fixed
- ArtistIdentity port struct missing Area/ArtistType → added
- FetchFirstTrackID added to Deezer adapter for ISRC extraction

## Known bugs to fix next session

### Bug 1: ISRC registrant set only collects one registrant
**File:** `services/go-api/internal/discovery/service/identity_resolver.go` ~line 128
**Problem:** `break` after finding first registrant means profile only knows J842. Che uses multiple distributors (J842 for main releases, SE6SA2 for DRUNKEN LOVE & HEARTBREAK).
**Fix:** Remove the `break`, iterate through several confirmed albums (e.g., first 5) to build a richer registrant set.
**Impact:** Some real Che albums may be incorrectly flagged as suspect by R3c.

### Bug 2: R3c suspect albums treated as unknown (not removed on first load)
**Designed behavior**, but user expects contamination removed immediately. The 24h safeguard means R3c-flagged albums show as "unknown" on first load. On second load (after 24h cache expiry + re-evaluation), they'd be removed.
**Decision needed:** Is the 24h safeguard worth the UX cost of showing suspect albums on first load?

### Bug 3: Cold-cache speed (~38 seconds)
**Root cause:** R2 (MB reverse-lookup) runs sequentially for every unconfirmed album at 1 req/sec. With 62 unconfirmed albums, that's ~62 seconds.
**Options:**
1. Skip R2 entirely when MB is rate-limiting (many 503s) — fall through to faster layers
2. Run R3 (iTunes) in parallel with R2 — iTunes has no harsh rate limit
3. Set a budget: run R2 for first N albums, skip rest if MB is slow
4. Background resolution: return confirmed albums immediately, resolve unknowns async

### Bug 4: 9 unknown albums need investigation
Need to identify which of the 9 unknowns are real Che albums vs contamination. Some may be:
- Real albums with ISRC registrant SE6SA2 (flagged as suspect by R3c, wrong)
- Contamination that escaped all layers (genuinely hard cases)
- Real albums not on iTunes and not in MB

## Key files changed

### New files
- `services/go-api/internal/shared/textnorm/normalize.go`
- `services/go-api/internal/discovery/domain/identity.go`
- `services/go-api/internal/discovery/domain/identity_test.go`
- `services/go-api/internal/discovery/adapters/cache/identity_cache.go`
- `services/go-api/internal/discovery/adapters/cache/identity_cache_test.go`
- `services/go-api/internal/discovery/service/identity_resolver.go`
- `services/go-api/internal/discovery/service/identity_resolver_test.go`
- `services/go-api/internal/discovery/service/identity_constraints.go`
- `services/go-api/internal/discovery/service/identity_constraints_test.go`

### Deleted files
- `services/go-api/internal/discovery/service/discography_filter.go`
- `services/go-api/internal/discovery/service/discography_filter_test.go`

### Key modified files
- `services/go-api/internal/discovery/service/get_artist_content.go` — replaced validateAndFilterDiscography with identity resolver
- `services/go-api/internal/discovery/adapters/providers/musicbrainz.go` — LookupAlbumArtist, area/type in identity, error on 503
- `services/go-api/internal/discovery/adapters/providers/itunes.go` — LookupAlbum, stripITunesTypeSuffix
- `services/go-api/internal/discovery/adapters/providers/deezer.go` — FetchTrackISRC, FetchFirstTrackID, genre_id
- `services/go-api/internal/discovery/adapters/providers/discogs.go` — genre/country extraction
- `services/go-api/internal/discovery/ports/ports.go` — IdentityResolver port, AlbumResolution, extended ArtistIdentity
- `services/go-api/internal/app/app.go` — IdentityResolverService wiring

## How to verify

```bash
cd services/go-api
go build -o ./tmp/api.exe ./cmd/api
go test ./internal/discovery/... -count=1 -timeout=120s

# Clear identity cache before testing
docker exec altune-redis-dev redis-cli EVAL "local keys = redis.call('keys', 'discovery:identity:v1:*'); for i,k in ipairs(keys) do redis.call('del', k) end; return #keys" 0

./tmp/api.exe
# Search "Che" → tap artist → check discography
# Expect: confirmed albums first, contamination removed, ~9 unknown at bottom
```

## Recommended next session approach

1. **Fix Bug 1** (ISRC registrant set) — collect from multiple confirmed albums
2. **Investigate the 9 unknowns** — identify which are real vs contamination
3. **Fix Bug 3** (speed) — run iTunes in parallel with MB, or skip R2 when MB is unreliable
4. **Decide on Bug 2** (24h safeguard UX) — is it acceptable?
5. **Test with other artists** — Aurora, Banks, Common to verify generalization
6. **Commit and write handoff**

## Docs written this session
- `docs/brainstorms/2026-06-19-identity-resolution-v3-requirements.md`
- `docs/plans/2026-06-19-002-feat-identity-resolution-v3-plan.md`
- `docs/handoffs/identity-resolution-v3-2026-06-19.md` (this file)
