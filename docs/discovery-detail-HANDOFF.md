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
card went **89 → 25 items, zero cross-artist leakage**).

**2026-07-23 session shipped four more fixes (all live on prod):** Spotify content
(pathfinder, not the dead classic API — §5.1); album tracklists showing the wrong
artist (native `GetAlbumTracks` for spotify/apple/soundcloud + artist-guarded
fallback — §5.6a); the SoundCloud discography gap (MB-bridged SC id + standalone
uploads as singles, so Che's "14 HAHAHA LOL" appears — §5.5); and **the permanent
identity-bridge fix** (verify-on-persist, now enabled — §6). Still open: top-tracks
fracture (§5.2), coarser bridged-card buckets (§5.3), client double-merge (§5.4,
needs EAS), existing-bad-row cleanup (§6 caveat).

**Flags (all `true` in prod `.env.production`, on the VM only):**
`DETAIL_IDENTITY_FIRST=true`, `DISCOGRAPHY_V2=true`, `IDENTITY_VERIFY_ON_PERSIST=true`.

**Test artist "Che" (SoundCloud-native Atlanta rapper):**
- Real Deezer artist id: **`399574001`**
- MusicBrainz MBID: **`0a68f3b5-79c2-4f81-a7bc-ebc977602e86`** ("Che | Atlanta rapper")
- The *wrong* Deezer id MB links to: **`234701081`** (a different, soul/R&B "Che")

---

## Update — 2026-07-23 (session 2): fracture verified closed + V2 consolidated

Two prod checks and a consolidation this session:

- **Top-tracks fracture (§5.2) no longer reproduces — closed.** Verified live: Che's
  bridged card (`deezer/234701081`) top-tracks are **all the rapper**, multi-sourced
  from apple/spotify/soundcloud/lastfm with **no deezer source** and zero soul-Che
  leakage; albums identical; metadata (year/track_count/record_type) complete (nested
  under `extras`). Mechanism: verify-on-persist (§6) already dropped the soul-deezer
  edge from Che's stored identity, so *neither* endpoint fans out to it — the fix at
  the identity layer subsumes both endpoints, and the album read-time MB anchor isn't
  even exercised for Che anymore. The standalone top-tracks read-time guard is now
  defense-in-depth for a verify-on-persist fail-open, not a live bug — **not built**
  (optional; if ever wanted, reuse the album surviving-provider set, not a new anchor).
- **verify-on-persist load-check (§6 caveat) — PASSED, flag stays on.** 72h of prod
  logs: `verify_dropped_edge` fired once, **zero** `identity.verify` errors, no MB
  429s. Low-load and clean.
- **Consolidation shipped (this commit):** flipped `DETAIL_IDENTITY_FIRST` +
  `DISCOGRAPHY_V2` to **code-default `true`** and retired the dead pre-V2 identity path
  (`identityAlbums` / `identityTopTracks` / `hideBareAlbums` + `sortByAgreement` /
  `bestRankOf`) and its 4 tests. `GetAlbums`/`GetTopTracks` are now just
  V2-identity-first → single-provider fallback. **Behavior-neutral on prod** (prod
  already ran both flags `=true`) — it makes V2 the code default and deletes
  unreachable code. Both flags kept as env kill-switches, though now redundant (both
  must be on for V2; can collapse to one or fully remove later). The shared
  `ConsensusService` stays (single-provider fallback + search album-validation still
  use it). `IDENTITY_VERIFY_ON_PERSIST` left as-is (env-gated, `=true` on prod). **1412
  backend tests green.** okf updated: `shared-infra.md` (hook-forced by config.go),
  `app-wiring.md` (accuracy).
  - *Prod `.env.production` note:* `DETAIL_IDENTITY_FIRST=true`/`DISCOGRAPHY_V2=true`
    are now redundant with the code default (harmless — env just matches). No VM edit needed.

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
2. ~~**Top-tracks fracture not covered.**~~ **VERIFIED NOT REPRODUCING (2026-07-23
   session 2 — see top).** verify-on-persist subsumed it at the identity layer; the
   standalone read-time guard (track-level MB-recording anchor or the album
   surviving-provider set) is now optional defense-in-depth, not a live bug.
3. **Coarser buckets on a *bridged* card.** Going id-only left only the surviving
   provider's `record_type`; Apple only flags "single" vs "album", so a 5-track EP
   can show as "album" (Deezer's finer type went with its dropped group). The
   rapper's *own* card (Deezer kept) buckets correctly.
4. **Client double-merge (F1) still present.** The mobile client still fans out to
   deezer + soundcloud + itunes separately and re-merges (`useArtistContent.ts`,
   `dedupAlbumsByTitle`). It's now non-lossy (each backend response is complete) but
   redundant. Removing it (single call, render verbatim) needs an **EAS build** to
   ship — untestable by us server-side.
5. ~~**Search-time SoundCloud id acquisition** never built.~~ **FIXED (2026-07-23,
   `musicbrainz_enrichment.go` + `soundcloud.go`).** The V2 discography disables the
   blind SoundCloud name-resolve (contamination-safety) and only queries a provider
   by a *persisted* id — but `externalIDsFromRelations` never extracted MusicBrainz's
   `soundcloud` url-relation, so the xref carried no SC id and SoundCloud sat out
   (zero SC sources on the bridged Che card). Fix: extract the `soundcloud` relation
   (the profile **handle**, e.g. `che`); `SoundCloudAPIAdapter.resolveUserID`
   resolves handle→numeric id via `/resolve` on use. SoundCloud now joins the id
   fan-out **verified** (from the MBID's own relation) and survives the MB anchor (7
   of Che's 9 SC albums overlap MB's release-groups, over the threshold of 4).
   **Plus** `GetArtistAlbums` now returns the typed playlists (album/ep/single) AND
   standalone track uploads not in any playlist as singles — SoundCloud has no
   release objects, so a genuine single is just an ungrouped upload; without this
   SC-exclusive drops (Che's "14 HAHAHA LOL") never reached the discography. Verified
   on prod: Che → `{soundcloud:13, spotify:29, applemusic:25}`, "14 HAHAHA LOL" in the
   singles row. *(Rollout note: an artist enriched **before** this change needs its
   14-day enrichment cache to refresh — or a manual re-enrich by MBID — before SC
   appears; new artists get it automatically.)*
6a. ~~**Album tracklists showed the wrong artist.**~~ **FIXED (2026-07-23,
   `spotify_content.go` + `applemusic.go` + `soundcloud.go` + `get_album_tracks.go`).**
   Opening an identity-bridged album (Che's EP "Empty Clip") rendered a *different*
   artist's tracklist ("Empty Clip" by Chase Fetti): only Deezer/iTunes implemented
   `GetAlbumTracks`, but bridged cards are apple/spotify/soundcloud-sourced (the
   deezer group was anchor-dropped), so they fell to a **blind Deezer title search**
   that returned a same-titled album by anyone. Fix: native `GetAlbumTracks` for
   Spotify (pathfinder `queryAlbumTracks`), Apple (catalog `/albums/{id}/tracks`), and
   SoundCloud (playlist tracks, or a single's track id as itself), all wired into the
   album-tracks map; plus an **artist-match guard** on the Deezer fallback (return
   empty, never a wrong-artist album). A follow-on SoundCloud regression — a single's
   tracklist showed "REST IN BASS" — was the same root cause, fixed by SC's
   `GetAlbumTracks`. Verified on prod for all three providers.
6. **Deferred extraction items** (doc §4): Deezer genre-id→name, Apple artwork
   resolution (capped 500px) + multi-genre, Spotify *search* album-artist (touches
   search merge keys → needs the discoveryeval gate), TheAudioDB fields.

---

## 6. The PERMANENT fix — DONE + ENABLED (2026-07-23)

**IMPLEMENTED and live on prod** (`identity_verify.go`, wired in
`search_wiring.go`, flag `IDENTITY_VERIFY_ON_PERSIST=true` set in the VM's
`.env.production`).

`stampIdentities` used to persist MusicBrainz's raw url-relations as the xref,
unverified — so a wrong streaming link (the wrong Deezer `234701081`) fused two
same-name artists in the durable identity. Now, in the **background** bridge
persist, `IdentityVerifier.VerifyXref` checks each streaming edge
(deezer/spotify/apple) against the artist's MB release-groups — the same
`groupMatchesAnchor` overlap test the detail anchor uses, applied per edge,
memoized per MBID — and **drops a non-overlapping (mis-bridged) edge before it is
stored.** Fail-open throughout; it filters only the persisted copy, never the
in-flight `Xref`, so merge/ranking (and the `discoveryeval` gate) are untouched.

**Verified on prod:** a `che` search logged
`identity.verify_dropped_edge mbid=0a68f3b5… provider=deezer external_id=234701081`
— the wrong Che rejected from the rapper's bridge. The discography stayed intact
(`{soundcloud:13, spotify:29, applemusic:25}`). The detail-time
`FilterGroupsByMBAnchor` is now a belt-and-suspenders guard, not the primary fix.

**Two caveats (still open):**
- **Existing bad rows aren't retroactively purged.** The verifier stops *new*
  fusions and cleans the freshly-stamped xref, but Che's already-persisted
  `deezer:234701081 → 0a68f3b5` row lingers (opening *that* card still resolves via
  it; the detail anchor keeps it clean). A one-time cleanup migration (delete stale
  edges whose catalogue fails the overlap test) would fully scrub the store.
- ~~**Enabled without a load-measurement pass.**~~ **MEASURED 2026-07-23 (session 2) —
  PASSED, flag stays on** (72h of prod logs: 1 dropped edge, zero `identity.verify`
  errors, no MB 429s). It adds a few background MB/provider fetches per newly-learned
  artist identity (memoized, off the request path, fail-open). Only deezer/spotify/apple
  edges are verified (soundcloud is an MB-authoritative handle; discogs/wikidata aren't
  catalogues).
- **Report the MB error upstream** (still worth doing): MusicBrainz accepts
  corrections; the wrong deezer url-relation on `0a68f3b5` should be removed.

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
