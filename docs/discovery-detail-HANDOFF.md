# Discovery detail / discography — session handoff (2026-07-23)

Pick-up doc for the next session. Deep design + diagrams live in
[`discovery-detail-pipeline.md`](discovery-detail-pipeline.md) (§6 = the rebuild,
§7 = the identity-fracture fix). This doc is the operational summary: what we did,
how to reproduce, what's open, and the permanent fix.

---

## 0. TL;DR

We rebuilt the artist **detail / discography** path from scratch (behind flags,
live on prod) and chased a "Che" contamination bug to its real root: a **MusicBrainz
data error** that fuses two same-name artists into one identity. Fixed it with
**MB-anchored identity verification**, verified on the live API (the bridged Che
card went **89 → 25 items, zero cross-artist leakage**). Several real follow-ups
remain (top-tracks not yet verified; the permanent upstream fix isn't built).
(Spotify content — previously dead — was fixed 2026-07-23; see §5.1.)

**Flags (both `true` in prod `.env.production`, on the VM only):**
`DETAIL_IDENTITY_FIRST=true`, `DISCOGRAPHY_V2=true`.

**Test artist "Che" (SoundCloud-native Atlanta rapper):**
- Real Deezer artist id: **`399574001`**
- MusicBrainz MBID: **`0a68f3b5-79c2-4f81-a7bc-ebc977602e86`** ("Che | Atlanta rapper")
- The *wrong* Deezer id MB links to: **`234701081`** (a different, soul/R&B "Che")

---

## 1. How to reach prod for debugging (IMPORTANT — this unblocked everything)

The detail endpoints sit behind `/v1` Supabase-JWKS auth, so you can't curl them
raw. Mint a real token with the **E2E fixture account** in
`apps/mobile/.env.local` (`E2E_FIXTURE_EMAIL` / `E2E_FIXTURE_PASSWORD` /
`EXPO_PUBLIC_SUPABASE_ANON_KEY` / `EXPO_PUBLIC_SUPABASE_URL`):

```bash
cd apps/mobile && eval $(grep -E '^(E2E_FIXTURE_EMAIL|E2E_FIXTURE_PASSWORD|EXPO_PUBLIC_SUPABASE_ANON_KEY|EXPO_PUBLIC_SUPABASE_URL)=' .env.local | sed 's/^/export /')
TOK=$(curl -s "$EXPO_PUBLIC_SUPABASE_URL/auth/v1/token?grant_type=password" \
  -H "apikey: $EXPO_PUBLIC_SUPABASE_ANON_KEY" -H "Content-Type: application/json" \
  -d "{\"email\":\"$E2E_FIXTURE_EMAIL\",\"password\":\"$E2E_FIXTURE_PASSWORD\"}" | python -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

# discography (albums) / top tracks — note: curl to a FILE then parse (responses have raw control chars)
curl -s -H "Authorization: Bearer $TOK" "https://altune.duckdns.org/v1/discovery/artists/deezer/399574001/albums?name=che&limit=100" -o alb.json
curl -s -H "Authorization: Bearer $TOK" "https://altune.duckdns.org/v1/discovery/artists/deezer/399574001/top-tracks?name=che&limit=10" -o tt.json
```

Provider-direct curls used for identity debugging (no auth):
- Deezer artist albums: `https://api.deezer.com/artist/<id>/albums?limit=100`
- MB url-relations: `https://musicbrainz.org/ws/2/artist/<mbid>?inc=url-rels&fmt=json` (needs a `User-Agent`)
- MB release-groups: `https://musicbrainz.org/ws/2/release-group?artist=<mbid>&limit=100&fmt=json`
- MB recording ISRCs: `.../recording?artist=<mbid>&inc=isrcs&limit=100&fmt=json`

---

## 2. Deploy workflow (learn from our mistakes)

- **Pushing to `main` auto-triggers the GitHub Action `deploy-backend.yml`** (SSH →
  `git pull` → `docker compose build` → `up`). **Do NOT also run `scripts/deploy.sh`
  manually** — the two race on the container recreate and the Action reports a red ✗
  (that was the "deployment failed" red herring, and the recurring name-conflict).
  Just push and watch: `gh run watch <id>`.
- The auto-deploy build has occasionally shipped a **stale binary** (cached Go
  layer). If prod behavior doesn't match the committed code, force it:
  `ssh … 'cd services/go-api && docker compose -f docker-compose.prod.yml build --no-cache go-api && docker rm -f altune-go-api && docker compose -f docker-compose.prod.yml up -d --force-recreate go-api'`.
- Flags live **only** in the VM's `services/go-api/.env.production` (gitignored).
  `docker compose up -d` does NOT recreate on an env-file change alone — a fresh
  image (from a deploy) does. To toggle a flag: edit the file, then recreate.
- Migrations are manual (none needed this session).

---

## 3. What we did (chronological, with commits)

The whole session, oldest → newest:

1. **`a94484b`** — threaded SoundCloud into the identity content fan-out (new
   `ports.ArtistIDResolver`, name→SC-id).
2. **`23d230c`** — SoundCloud metadata (release_date + track_count, previously
   dropped) + reverted an over-aggressive MB album purge.
3. **`64b7a6e`** — **reverted** a "protect-primaries" consensus change that caused a
   contamination regression (lesson: making id-primaries immune to the MB filter
   let same-name albums through).
4. **`f62c0eb`** — wrote the architecture doc + ran a **provider extraction audit**
   (found every provider was dropping displayable fields).
5. **`6ff3c59`** — extraction batch: YT Music explicit, Deezer explicit + 1000px
   artwork, iTunes record_type + track date, Apple record_type, MB recording
   duration + secondary-types, Spotify content album_type/artist/date.
6. **`5cbaff4`** — clean-slate **redesign spec** (doc §6).
7. **`dbe9dd5` / `4bd1bf4` / `cf2ce0b`** — the three **pure cores** (the correctness,
   exhaustively unit-tested): `MergeReleases` (field-level **best-of**),
   `KeepRelease`/`FilterKept` (keep on identity provenance), `NormalizeRecordType` /
   `BucketDiscography` (incl. the "1 track = single" rule).
8. **`f3695c2`** — wired V2 into `GetArtistContentService` behind `DISCOGRAPHY_V2`.
9. **`9a23f08`** — `KeepRelease = IDVerified` only (dropped the unsound
   HasStrongID / ≥2-provider rules — a namesake's album has its own valid MBID) +
   strip the " - Single"/" - EP" title suffix Apple/iTunes append.
10. **`454f2cc` / `2d73ca4`** — **the big one**: a diagnostic log revealed V2 had
    **never run for Che** — the identity-first block was gated on a durable
    IdentityStore bridge that underground artists don't have (`identity_ok=false`),
    silently falling back to the old path. Fix: run V2 on the **seed identity**
    regardless (the seed id is already id-verified). Removed the diagnostics.
11. **`f4a17a6` / `37426e2`** — provider-**cohesion** layer (connected components
    by shared release). **Abandoned** — same-name artists share coincidental titles,
    so title-overlap can't separate them. Code kept as a no-MBID fallback but it's
    ineffective for this case.
12. **`56bd812`** — **the fix**: MB-anchored identity verification (see §4).
13. **`8351610`** — doc update.

Key source files (all `services/go-api/internal/discovery/service/` unless noted):
`release_merge.go`, `release_keep.go`, `release_bucket.go`, `release_cohesion.go`,
`release_verify.go`, `get_artist_content_v2.go`, `get_artist_content.go`;
`adapters/providers/musicbrainz.go` (`ReleaseGroupTitles`);
`ports/ports_search.go` (`MBDiscographyAnchor`, `ArtistIDResolver`);
`internal/app/discovery_wiring.go` (wiring); `internal/shared/config/config.go`
(`DiscographyV2`).

---

## 4. The root cause and the fix (the part that mattered)

**Root cause = a MusicBrainz DATA error, not our pipeline.** MB `0a68f3b5`
("Che, Atlanta rapper") has url-relations to spotify/apple/soundcloud (all the
rapper ✓) **and to deezer `234701081` — a *different* Che** (soul/R&B; the
rapper's real Deezer is `399574001`). Our search persisted that bridge, so the
stored identity **fuses two humans**. Fanning it out returns both artists' catalogs,
each legitimately "id-verified." No per-release keep rule can separate them.

**What we ruled out (with live data — stop re-trying these):**
- **Title cohesion** — dead. The two Ches share coincidental single titles
  ("Baddest", etc.); no title-overlap threshold survives.
- **ISRC matching** — blocked. MB has ISRCs, but **Deezer's artist endpoints don't
  expose ISRC** (only per-track lookups do), so the suspect provider can't be checked.

**The reachable robust signal = the artist's own authoritative MB catalogue.**
Live-measured: the mis-bridged soul-Deezer shares **8%** of the rapper's MB
release-groups; the real rapper-Deezer shares **60%**. Huge margin.

**`FilterGroupsByMBAnchor`** (`release_verify.go`): for each id-fanout provider
group, compute title overlap with the MBID's MB release-group set; **drop** a
group whose overlap is below ~25% / 4 releases (a mis-bridged same-name artist).
Fail-open (no anchor / no MBID / MB error → keep all). Wired via
`ports.MBDiscographyAnchor` (`MusicBrainzAdapter.ReleaseGroupTitles`) +
`WithMBAnchor(sharedMB)`. Also made `v2Albums` **id-only** — the by-name
completeness feed was itself a contamination source (iTunes searched the *name*
"che" and returned a different artist + title collisions).

**Verified on prod:** bridged card `234701081` → **89 → 25 items, 0 soul-Che
leakage**, rapper's real discography with year/tracks/covers. Rapper's own card
`399574001` unaffected (seed-only, no MBID → verification correctly skipped).

> ⚠️ The MB-anchor thresholds (`mbAnchorMinReleaseGroups=5`, `mbVerifyMinTitles=4`,
> `mbVerifyMinOverlap=4`, `mbVerifyMinRatio=0.25`) are tuned to the 8%-vs-60% gap.
> They're documented in `release_verify.go`; revisit if a legit artist with a
> platform-exclusive catalogue gets wrongly dropped.

---

## 5. STILL OPEN (prioritized)

1. ~~**Spotify content is DEAD.**~~ **FIXED (2026-07-23, `spotify_content.go`).**
   The 429 hypothesis was wrong. The token was never the problem — content was
   calling the **classic Web API** (`api.spotify.com/v1`), Spotify's developer-OAuth
   API, where the anonymous web-player token has ~zero quota (every call 429s "rate
   limit exceeded" even from a cold IP, deterministically — retry/backoff would just
   delay a guaranteed failure). Search worked because it rides the **pathfinder
   GraphQL API** (`api-partner.spotify.com`), which the same token IS authorized for.
   Fix: moved both content endpoints onto pathfinder — `GetArtistAlbums` →
   `queryArtistDiscographyAll`, `GetArtistTopTracks` → `queryArtistOverview` (its
   `topTracks`). Verified live end-to-end via the real adapter (50 albums + 10 top
   tracks, with dates/artwork/track-counts). The persisted-query hashes are
   extractable from the public JS bundle (grep the linked `web-player.<build>.js`
   for `new <mod>.l("queryArtistDiscographyAll","query","<sha256>",null)`) — see the
   AIDEV-WARNING in `spotify_content.go`; a stale hash returns HTTP 412 "Invalid
   query hash", not an auth status. A `SPOTIFY_LIVE=1`-gated E2E smoke test guards
   against silent hash-rotation regression.
2. **Top-tracks fracture not covered.** The MB anchor is album-level, so a bridged
   card's *top tracks* can still mix two artists. Need a track-level anchor (MB
   recording titles/ISRCs) or apply the album-verification's surviving-provider set
   to the top-tracks fan-out.
3. **Coarser buckets on a *bridged* card.** Going id-only left only the surviving
   provider's `record_type`; Apple only flags "single" vs "album", so a 5-track EP
   can show as "album" (Deezer's finer type went with its dropped group). The
   rapper's *own* card (Deezer kept) buckets correctly.
4. **Client double-merge (F1) still present.** The mobile client still fans out to
   deezer + soundcloud + itunes separately and re-merges (`useArtistContent.ts`,
   `dedupAlbumsByTitle`). It's now non-lossy (each backend response is complete) but
   redundant. Removing it (single call, render verbatim) needs an **EAS build** to
   ship — untestable by us server-side.
5. **Search-time SoundCloud id acquisition** never built (persist SC id when it
   co-identifies at search, so SC joins the id fan-out instead of being name-guessed).
6. **Deferred extraction items** (doc §4): Deezer genre-id→name, Apple artwork
   resolution (capped 500px) + multi-genre, Spotify *search* album-artist (touches
   search merge keys → needs the discoveryeval gate), TheAudioDB fields.

---

## 6. The PERMANENT fix (recommended next major piece)

The detail-time MB-anchor is a **defense**. The disease is upstream: the search
persists a cross-provider identity bridge **without verifying the providers are the
same artist**. Fix it at the source:

- **At search / `stampIdentities` (identity persist time): verify catalogue overlap
  before storing a bridge.** If a provider id from MB's url-relations resolves to an
  artist whose releases don't overlap the MBID's (the 8%-vs-60% test), **don't
  persist that (provider→MBID) edge.** Then the fractured identity is never stored,
  the search would surface two *separate* "Che" cards, and detail inherits a clean
  identity — no downstream defense needed. This also fixes the *search* results, not
  just detail.
- **Report the MB error upstream.** MusicBrainz accepts corrections; the wrong
  deezer url-relation on `0a68f3b5` should be removed. External + slow, but it's the
  actual data bug.

Then the detail-time `FilterGroupsByMBAnchor` becomes a belt-and-suspenders guard
against *new* un-verified bridges rather than the primary fix.

---

## 7. Things easy to forget / gotchas

- **No detail-path eval harness exists.** `discoveryeval` covers **search ranking**,
  not detail/discography — so every detail change was verified by hand (unit tests +
  the live-API token trick). Building a detail eval (feed known artists, assert clean
  discographies) would end the manual loop. None of our changes touched search
  ranking, so the `discoveryeval` gate didn't apply — but if you touch search-path
  mappers or merge keys, it does (see `internal/discovery/CLAUDE.md`).
- **"Song" is banned** — the noun is `Track` (project ubiquitous-language rule).
- **Provider topology** (who's queried how):
  - *Search fan-out*: Deezer, Apple Music, MusicBrainz, Last.fm, SoundCloud, YouTube
    Music, Amazon, Spotify (by string).
  - *V2 discography id fan-out*: Deezer, Apple Music (via shared iTunes id), Spotify
    (dead), SoundCloud (if id in xref), Last.fm (by MBID — note: Last.fm
    `gettopalbums` doesn't accept an MBID as the artist param, so it effectively
    returns nothing there). **iTunes is NOT in the id fan-out** (Apple Music replaced
    it) — it was only ever a by-name consensus provider, now removed from V2.
  - *Old by-name consensus* (lastfm/mb/discogs/itunes/ytmusic/soundcloud): still used
    by the **non-V2 fallback** and the search album-validation, but **V2 no longer
    uses it** for the discography.
- **Redis caches are app-wide, not per-user.** Identity store bridges + MB
  release-group memo (6h TTL) are shared.
- **736 backend tests green** as of `8351610`; `go test ./internal/discovery/... -count=1`.
- The `MEMORY.md` entry `identity-first-detail.md` has the running blow-by-blow if
  this doc misses anything.
