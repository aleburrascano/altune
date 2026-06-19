---
date: 2026-06-19
topic: identity-resolution-v3
parent: 2026-06-19-artist-identity-v2-requirements.md
---

# Artist Identity Resolution v3 — Eliminate Thresholds

## Summary

Replace the threshold-based discography contamination filter with a multi-provider identity resolution system. Instead of scoring albums for removal, resolve which artist an album belongs to across all available providers. The decision is binary: same artist (keep) or different artist (remove). Albums unknown to all providers are optimistically included.

## Problem Frame

The v2 system uses a mismatch-count threshold (`mismatch >= 2`) to remove contaminated albums. This fails for niche artists where only one cross-reference source is available: with a single mismatch signal, contamination survives. The threshold is arbitrary, hardcoded, and doesn't scale — lowering it increases false positives, raising it lets more contamination through.

The deeper issue: the system tries to decide "should we remove this album?" when it should be asking "who does this album belong to?" Every provider API returns rich identity metadata that can answer that question directly.

## Core Principle

**All metadata from every registered provider feeds artist identity resolution. The contamination decision is binary (same artist / different artist), not scored.**

No thresholds. No regex. No per-artist tuning. The system should generalize to any same-name artist collision.

## Requirements

### R1. Build a rich artist identity profile

For the target artist, extract and cache identity signals from every provider that has data:

- **MusicBrainz**: MBID, disambiguation, area (city/country), birth year, type (person/group), genre tags
- **Discogs**: artist ID, genre, country, profile text, styles
- **Last.fm**: genre tags, similar artists
- **Deezer**: genres of confirmed albums (via genre_id), nb_fan
- **iTunes**: primaryGenreName
- **SoundCloud**: city, genre

This profile is **who our artist is**. It feeds disambiguation in R2, genre compatibility checks in R3, constraint checks in R3b, and the ISRC registrant fingerprint in R3c.

### R2. MBID reverse-lookup per unconfirmed album

For each album not confirmed by existing cross-reference (MB release-groups or Discogs releases):

1. Search MusicBrainz: `artist:"ArtistName" release-group:"AlbumTitle"`
2. If results exist, disambiguate which MB artist is our target using the identity profile from R1 (match on MBID, area, type, genre overlap)
3. **Decision**:
   - Album credited to our artist's MBID → **confirmed** (keep, promote to confirmed ordering)
   - Album credited to a different MBID → **contamination** (remove)
   - No MB results → fall through to R3

### R3. Cross-provider album search

For albums that escape R2 (not in MB, or MB has no data), search for the album title across all registered providers and check who it's credited to:

- **iTunes**: search `term=AlbumTitle&entity=album`. Three outcomes:
  - Credited to a **different artist name** → contamination (e.g., "LOTTO DREAMS" by "Mr. E.L.Y")
  - Credited to the **same name but incompatible genre** → contamination (e.g., "Tšernobõl" by "Che" with genre "Rock" vs our Hip-Hop profile)
  - Credited to the **same name with compatible genre** → **confirmed** (promotes to confirmed set, gets visual priority)
- **Discogs**: if we have a resolved Discogs artist ID (from v2):
  - Album exists under a different Discogs artist ID → contamination
  - Album exists under our Discogs artist ID → **confirmed**

R3 is **bidirectional**: it can confirm albums (grow the confirmed set) or flag contamination (remove). This matters because MB may not have an album but iTunes might — confirmed albums get visual priority over unknowns.

Deezer is excluded from cross-provider album search — it's the conflating provider. Searching Deezer for a contamination album returns the same conflated artist ID, which is not useful as a cross-reference.

**Evidence from API probing (2026-06-19):**
- "LOTTO DREAMS" → iTunes credits it to "Mr. E.L.Y" (artist_id=415339886), not Che → contamination
- "Tšernobõl" → iTunes credits it to "Che" (artist_id=1463707741) with genre "Rock" → same name, incompatible genre → contamination
- "Samsonite" → not found on iTunes → falls through to R3b

### R3b. Identity profile constraint checks (fallback)

**Why this exists:** MB probe (2026-06-19) showed that all four known contamination albums for "Che" (Samsonite, Gallos Ciegos, LOTTO DREAMS, Kiss Me in the Sky) do not exist in MusicBrainz under any artist. R2 returns "no results" for all of them. R3 also fails when Discogs resolves to the wrong artist (overlap=0). A fallback is needed.

For albums that escape both R2 and R3 (no provider has definitive identity data), apply binary constraint checks derived from the identity profile (R1). Each constraint is individually decisive — not scored, not counted:

1. **Temporal impossibility**: Album year < artist birth year → the artist did not exist when this album was released → contamination. (Example: Che born 2006, "Samsonite" released 1995.)
2. **Artist type mismatch**: Identity profile says "person" (solo), album's credited artist on another provider is a "group" (band), or vice versa → contamination.
3. **Genre cluster incompatibility**: The artist's genre tags across ALL providers form a cluster (e.g., {hip-hop, rap, trap}). The album's genre tags (from Deezer genre_id, or from the album's credited artist's tags on another provider) have zero overlap with that cluster → contamination. Zero overlap means no shared genre across any provider — not a threshold, a set intersection that is empty.

Each check is binary: violated = contamination, not violated = keep. If none are violated, the album passes to R4.

### R3c. ISRC registrant fingerprint

**Why this exists:** API probing (2026-06-19) showed that Samsonite, Gallos Ciegos, and Kiss Me in the Sky escape R2, R3, and R3b — they're not in MB, not in iTunes, not caught by temporal checks. But their ISRC codes reveal they were registered by different distributors than Che's real music.

ISRC format: `CC-XXX-YY-NNNNN` where CC=country, XXX=registrant (label/distributor). Build a registrant fingerprint from confirmed albums' tracks:

1. Fetch the ISRC from the **first track** of each confirmed album (one Deezer `/track/{id}` call per album — all tracks on an album share the same registrant).
2. Extract the registrant code (characters 3-6 of the ISRC). Collect all unique registrants into the artist's **known registrant set**.
3. For unconfirmed albums: fetch the ISRC from the first track. If the registrant code is not in the artist's known set → suspect (first encounter) or contamination (re-evaluation).

**Evidence from API probing (2026-06-19):**
- All confirmed Che albums (REST IN BASS, Sayso Says, closed captions, Fully Loaded): ISRC registrant = `J842` (US digital distributor)
- Gallos Ciegos: registrant = `FZ62` → different distributor → contamination
- Kiss Me in the Sky: registrant = `K6K2` → different distributor → contamination  
- Samsonite: registrant = `7812`, country = `CH` (Switzerland) → different country and distributor → contamination

**Safeguard against distributor changes:** R3c does NOT remove on first encounter. It marks the album as "suspect" → placed in the unknown bucket (visible, deprioritized below confirmed). On the next cache re-evaluation (24h later per R6's unknown TTL), if the album is STILL not confirmed by R2/R3 AND R3c still fires → remove as contamination.

Why this is safe: a legitimate new album on a new distributor will be catalogued by MB/iTunes/Discogs within days — R2/R3 will confirm it before R3c can remove. Contamination from years ago will never be catalogued under the correct artist — after 24h it gets removed permanently (30-day cache TTL on contamination results).

When an album is confirmed via R2/R3, its ISRC registrant gets added to the artist's known registrant set, so future albums from the same distributor are immediately safe.

### R4. Optimistic include for unknown albums

Albums that pass all checks (R2 no data, R3 no data, R3b no constraint violations, R3c no ISRC mismatch) are kept. These are likely legitimate new releases not yet catalogued. The existing confirmed-first ordering provides visual prioritization: confirmed albums appear first, unknown albums appear after.

### R5. Replace FilterContamination with identity-resolution approach

Remove the threshold-based `FilterContamination` function and `DiscographyFilterInput` struct. Replace with an `IdentityResolver` that takes the artist identity profile and a list of albums, and returns `{confirmed: [], contamination: [], unknown: []}`. The caller orders results as confirmed → unknown (contamination removed entirely).

### R6. Cache identity resolution results

Per-album identity lookups are expensive (1+ API call each). Cache results:
- Positive (confirmed or contamination): 30-day TTL
- Unknown: 24-hour TTL (re-check as providers catalogue new releases)

### R7. Rate-limit aware batch processing

MB and Discogs have rate limits (1 req/sec each). Process unconfirmed albums concurrently across providers but respect per-provider rate limits. Use the existing rate limiter infrastructure.

### R8. Squeeze provider metadata extraction

Audit every registered provider adapter and extract ALL available metadata fields into `SearchResult.Extras`. Specifically:
- Deezer: `genre_id`, `contributors[]`, `nb_fan` on albums
- Last.fm: `tags[]` on artists and albums
- MusicBrainz: `tags[]`, `area`, `type` on artists; `primary-type` on release-groups
- iTunes: `primaryGenreName` on albums
- Discogs: `genres[]`, `styles[]`, `country` on artists

This data feeds the identity profile (R1) and disambiguation (R2).

## Non-goals

- Audio fingerprinting (deferred until acquisition pipeline)
- ML/model-based classification (deferred until team grows)
- Frontend changes (backend-only)
- Adding new provider integrations (better use of existing ones)

## Acceptance Examples

- **AE1**: Search "Che" → tap artist → discography shows REST IN BASS, Sayso Says, closed captions, REST IN BASS: ENCORE. Does NOT show Samsonite (ISRC mismatch — R3c), Gallos Ciegos (ISRC mismatch — R3c), Tšernobõl (iTunes genre mismatch — R3), LOTTO DREAMS (iTunes different artist — R3).
- **AE2**: Search "Aurora" → tap artist → Norwegian pop discography, no contamination from other Auroras.
- **AE3**: Search "Banks" → tap artist → R&B/electronic discography, no contamination from other artists named Banks.
- **AE4**: A brand-new album by Che released today (not yet in MB or Discogs) appears in the discography below confirmed albums. It is NOT removed.
- **AE5**: No hardcoded artist names, thresholds, or regex anywhere in the identity resolution code.

## Key Decisions

1. **Binary identity, not scored filtering.** The removal decision is "same artist or different artist," not "enough mismatch signals." Metadata feeds disambiguation quality, not removal thresholds.
2. **Identity profile as fallback decision-maker.** When no provider has definitive data (R2/R3 return nothing), the identity profile's constraint checks (R3b) make binary impossibility decisions — not heuristic scores.
3. **Optimistic include for unknowns.** New releases that pass all constraint checks are kept, not removed. Confirmed-first ordering handles visual priority.
4. **Multi-provider identity, not single-source.** MB is the primary identity source (MBID is the gold standard), but Discogs artist ID and Last.fm artist identity serve as independent checks when MB has no data.
5. **Cold-cache penalty is acceptable.** First discography load for a niche artist may take 30-60s (per-album API calls at 1 req/sec). Subsequent loads are cached (30-day TTL). UX should show confirmed albums immediately while resolving the rest.

## Open Questions

- **Q1**: How many unconfirmed albums does a typical niche artist have? This determines the API call budget per discography load. (Measure on Che, Aurora, Banks, Common.)
- **Q2**: ~~Should identity results be cached per-album or per-artist-album pair?~~ **Resolved: per-artist-album pair.** The same album title can exist under different artists; caching per-album risks false sharing.
- **Q3**: Last.fm artist-level tag comparison — is this worth the API call, or does MB + Discogs + iTunes cover enough ground? (Grilling showed iTunes alone catches 2 of 4 hardest contamination albums.)

## Success Criteria

- Zero contaminated albums visible for Che, Aurora, Banks (test with `/test-search`)
- New legitimate releases (not yet in any provider) still appear in discography
- No thresholds, regex, or hardcoded values in the identity resolution code path
- No regression in search pipeline positioning tests (canonical queries from `CLAUDE.md`)
- Identity resolution adds < 2s to discography load time on cached loads; first load shows confirmed albums immediately, resolves unconfirmed in background (30-60s for niche artists)
