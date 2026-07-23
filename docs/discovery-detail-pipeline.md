# Discovery detail / discography pipeline — architecture

> Living design doc (2026-07-23). Purpose: see the whole detail/discography flow at once so we can decide which decision-layers to keep, merge, or kill — instead of patching one layer and breaking another (which is exactly how the "wrong-Che discography" regression happened). Pairs with the OKF concept docs (`okf/backend/discovery/`), which describe each subsystem in prose; this doc is the *map between them*.

---

## 1. The two paths

There are two distinct request flows. They share providers and the `Merge` core, but they are **not** the same pipeline.

```mermaid
flowchart TB
    subgraph SEARCH["SEARCH PATH — user types a query"]
        Q[query string] --> SF[fanOut: query every provider by NAME/STRING]
        SF --> STAMP[stampIdentities: attach MB cross-provider ids + persist to IdentityStore]
        STAMP --> MRG1[Merge → Rank → diversity → disambiguation → fillArtwork]
        MRG1 --> RES[ranked SearchResults\neach entity carries merged Sources]
    end

    subgraph DETAIL["DETAIL PATH — user taps an artist"]
        TAP[tap artist result\nseed = one provider + externalID + artistName] --> RID[resolveArtistIdentity\nIdentityStore.LookupByProviderID]
        RID --> FO[fanOutByIdentity: query each content provider by ITS OWN id]
        FO --> MRG2[Merge best-of]
        MRG2 --> CONS[BuildConsensus + MB-authority]
        CONS --> POST[normalizeYears → hideBareAlbums → sort]
        POST --> OUT[discography + top-tracks]
    end

    RES -.->|search writes the identity bridge\nthat detail later reads| RID
```

**Key insight:** the SEARCH path queries providers **by name/string** and leans on `Merge` + MB-bridge to resolve identity. The DETAIL path is supposed to be the opposite — query **by stable id** so a same-name artist can't bleed in. The bugs all live where the DETAIL path *falls back to name* (SoundCloud name-resolve; the by-name consensus fetch) and then can't tell the real artist from a namesake.

---

## 2. The detail / discography pipeline (the part we're redesigning)

```mermaid
flowchart TD
    START["GetAlbums / GetTopTracks\n(seedProvider, seedID, artistName)"] --> FLAG{DETAIL_IDENTITY_FIRST?}
    FLAG -->|no| SINGLE["single-provider path\n(fallback, MB-authority applies)"]
    FLAG -->|yes| RID["resolveArtistIdentity(seed)\n→ IdentityStore.LookupByProviderID"]

    RID --> OK{identity resolved?}
    OK -->|miss| SINGLE
    OK -->|hit| IDOBJ["ResolvedArtistIdentity\n{MBID, ProviderIDs xref}\n(seed always preserved)"]

    IDOBJ --> FANOUT["fanOutByIdentity — per content provider:"]

    subgraph L1["LAYER 1 — per-provider id resolution (providerContentID)"]
        direction LR
        P1["Deezer → xref id"]
        P2["Apple → iTunes id"]
        P3["Spotify → xref id"]
        P4["Last.fm → MBID"]
        P5["SoundCloud → ⚠ NO xref id\n→ resolveArtistIDByName (top search hit)"]
    end
    FANOUT --> L1

    L1 --> QUERY["query each provider's GetArtistAlbums/TopTracks by that id (concurrent)"]
    QUERY --> MERGE["LAYER 2 — Merge best-of\n(union sources, firstNonEmpty/firstNonZero on typed fields)"]

    MERGE --> BRANCH{albums or tracks?}

    BRANCH -->|tracks| SORTA["sortByAgreement\n(single-source sinks)"] --> TOUT["top tracks"]

    BRANCH -->|albums| CONS["LAYER 3 — BuildConsensus"]
    subgraph L3["LAYER 3 detail"]
        direction TB
        CFETCH["fetch each consensus provider BY NAME\n(lastfm, musicbrainz, discogs, itunes, ytmusic, soundcloud)"]
        CCLUS["cluster by exact canonical title"]
        CMB["⚠ LAYER 4 — applyMBAuthority:\nif MB confirms ≥1 album →\nREJECT every album MB doesn't credit"]
        CFETCH --> CCLUS --> CMB
    end
    CONS --> L3
    L3 --> HIDE["LAYER 5 — hideBareAlbums\n(drop no-cover AND no-year AND single-source)"]
    HIDE --> SORTB["LAYER 6 — normalizeYears → sortByReleaseDateDesc"]
    SORTB --> AOUT["discography"]
```

### What each layer does — and its failure mode

| Layer | Purpose | Failure mode we've hit |
|------|---------|------------------------|
| **1. Identity id-resolution** (`providerContentID`, `resolveArtistIDByName`) | Get each provider's own id for the artist so we query by id, not name | MB xref never carries a **SoundCloud** id → SC falls back to **name-resolve (top hit)**, which for an ambiguous name ("Che") can be the **wrong person** |
| **2. Merge best-of** (`mergeInto`) | Union provider results; fill each field from whichever provider has it | Sound — but only as good as its inputs. If a provider **drops a field at extraction** (SC year/track-count), best-of has nothing to fill from |
| **3. Consensus by-name** (`BuildConsensus`) | Add completeness: albums the id fan-out missed, fetched by artist name | Name fetch pulls in **same-name contamination**; relies on Layer 4 to filter it |
| **4. MB-authority** (`applyMBAuthority`) | Remove same-name contamination using MB's discography as the spine | **Double-edged**: also **purges REAL releases** MB simply hasn't catalogued (REST IN BASS: ENCORE). This is the load-bearing, dangerous layer |
| **5. hideBareAlbums** | Drop metadata-less broken cards | Mostly benign (SC always has artwork so it's rarely triggered) |
| **6. normalize + sort** | Year display + newest-first ordering | Fine |

---

## 3. Provider participation matrix

Which provider participates in which path, and **keyed by what**. "by id" = identity-safe; "by name" = contamination-prone.

| Provider | Search fan-out | Content fan-out (detail, **by id**) | Consensus (**by name**) | Artwork chain |
|----------|:---:|:---:|:---:|:---:|
| Deezer | ✅ (string) | ✅ id | seed only | ✅ (name) |
| Apple Music | ✅ (string) | ✅ (shared iTunes id) | — | — |
| iTunes | — | — | ✅ name | ✅ (name) |
| Spotify | ✅ (string) | ✅ id | — | ✅ artist img (id) |
| SoundCloud | ✅ (string) | ⚠️ **name-resolved id** | ✅ name | ✅ (name, last) |
| Last.fm | ✅ (string) | ✅ (MBID) | ✅ name | — |
| MusicBrainz | ✅ (string) | — (spine/authority) | ✅ name + **authority** | ✅ CAA (id) |
| YouTube Music | ✅ (string) | ❌ **not wired** | ✅ name | ✅ artist img (id) |
| Amazon Music | ✅ (string) | ❌ not wired | — | — |
| Discogs | — | — | ✅ name | ✅ (id) |
| TheAudioDB | — | — | — | ✅ (id) |

**Two things this table surfaces immediately:**
1. **YouTube Music is in search + consensus but NOT the content fan-out** — yet it maps `year` (we confirmed `mapYTMusicAlbum` sets `r.Year`). Its discography never reaches the detail screen by id.
2. **SoundCloud is the only content-fan-out provider reached by name**, not id — the single contamination vector in a path designed to be id-only.

---

## 4. Where display metadata gets lost (the extraction question)

The merge best-of (Layer 2) can only surface a field if **some provider's mapper extracted it**. The existing `docs/providers/maximization-audit-2026-06-22.md` audited **identity keys** (ISRC/UPC/MBID) and **coverage** — it did **not** audit displayable fields (year, track-count, duration, record-type, label, explicit). That's the gap the SoundCloud year/track-count miss lived in for weeks.

Field-level extraction audit (2026-07-23, code-cited). Ranked drops — a **DROPPED** field is returned by the API and thrown away; **PARTIAL** is decoded-but-lossy. These are fix targets:

| # | Provider | Field | Status | Site |
|---|----------|-------|:---:|------|
| 1 | **YouTube Music** | explicit flag (track **and** album) — *parsed into the struct, then ignored by the mapper* (the exact SoundCloud pattern) | DROPPED | `ytmusic.go:183-189, 232-235` (fields at `ytmusic_client.go:78,96`) |
| 2a | **Deezer** | track `explicit_lyrics` — not even decoded | DROPPED | `deezer.go:161-181, 105-117` |
| 2b | **Deezer** | album genre stored as raw numeric `genre_id`, not a display name | PARTIAL | `deezer.go:127-129` |
| 2c | **Deezer** | search results ship 500px art while 1000px (`cover_xl`) is already in hand | PARTIAL | `deezer.go:111,123,132` |
| 3a | **iTunes** | album `record_type` never set — despite the code already knowing the "- Single"/"- EP" suffixes | DROPPED | `itunes.go:160-168` (suffixes at `:368`) |
| 3b | **iTunes** | track release date omitted (album branch sets it, track branch doesn't) | DROPPED | `itunes.go:178-183` |
| 4a | **Spotify (content)** | `album_type` collapsed to single-only; album/compilation dropped | PARTIAL | `spotify_content.go:152-154` |
| 4b | **Spotify (content)** | album artist subtitle blank (artists[] not decoded) | DROPPED | `spotify_content.go:155` |
| 4c | **Spotify (content)** | track release date dropped (embedded album decodes only name+images) | DROPPED | `spotify_content.go:128-131` |
| 5a | **Apple Music** | album `record_type` single-only (regular albums/EPs unlabeled) | PARTIAL | `applemusic.go:273-275` |
| 5b | **Apple Music** | artwork hard-capped at 500px; Apple serves up to 3000px (contrast iTunes' 1500px hero) | PARTIAL | `applemusic.go:43,249,281,300` |
| 5c | **Apple Music** | only first genre kept | PARTIAL | `applemusic.go:232,264,292` |
| 6 | **MusicBrainz** | recording `length` (duration) not decoded; secondary-types (compilation/live) dropped | DROPPED | `musicbrainz.go:301-306, 353` |
| 7 | **Spotify (search)** | albums ship blank artist subtitle (query never requests artists) | DROPPED | `spotify.go:364, 283-290` |
| 8 | **TheAudioDB** | genre/bio/country/formed-year all dropped (marginal — artist-only, artwork use) | DROPPED | `theaudiodb.go:36-40` |

**Fixed 2026-07-23 (extraction batch):** SoundCloud `release_date`+`track_count`; YT Music explicit (#1); Deezer track explicit + 1000px artwork (#2a,2c); iTunes album record_type + track release date (#3a,3b); Spotify content album_type + album artist + track release date (#4a,4b,4c); Apple Music album record_type default (#5a); MusicBrainz recording duration + secondary-types (#6). **Deferred (tuning/risk, not dropped data):** Deezer genre_id→name (needs extra fetch, #2b); Apple artwork resolution + multi-genre (#5b,5c — touches shared payloads/consumer type); Spotify search album artist (#7 — touches search merge keys, needs eval gate); TheAudioDB (#8 — marginal). None of the shipped changes touch ranking inputs, so no eval-gate needed.

**Cross-cutting patterns the table reveals:**
- **"Parsed then discarded"** (YT explicit, the SC year miss): the field is *in the response struct*, the mapper just doesn't copy it. Cheapest, safest fixes.
- **`record_type` is unreliable across the board** — iTunes never sets it, Spotify/Apple only flag "single". This is *directly* why album/single/EP bucketing is "better but not perfect": most providers under-label, so the section grouping falls back to whatever one provider said.
- **Artwork resolution left on the table** — Deezer (500 vs 1000), Apple (500 vs 3000). Hero images render softer than they could.
- **Album artist blank on both Spotify paths** — a whole subtitle dropped.

---

## 5. Open design questions (what the diagram is *for*)

The core tension is **Layer 4 (MB-authority)**. It is simultaneously:
- the **only** thing filtering same-name contamination from the by-name consensus fetch, and
- a **purger of real releases** MB hasn't catalogued.

We tried making the id-fan-out primaries immune to it (`protectPrimaries`) → contamination leaked through because a wrong SC name-resolve produced "primaries" that weren't the artist's. **Reverted.**

The redesign should decouple those two roles. Candidate directions to evaluate against the diagram:

1. **Confidence/corroboration-based keeping** instead of MB-veto. Keep an album if it is (a) on ≥2 **identity-safe** providers (id fan-out agreement), OR (b) a high-confidence id match — *regardless* of MB. Reject only single-source **name-fetched** albums MB contradicts. This keeps ENCORE (2+ id providers) and drops namesakes (name-only, MB-contradicted).
2. **Make SoundCloud id-safe.** The name-resolve top-hit is the contamination root. Options: (a) skip SC name-resolve when MB flags the name as ambiguous; (b) verify the resolved SC artist against a corroborating signal (shared track title / ISRC) before trusting it; (c) persist the SC id into the IdentityStore during search-merge when SC provably merges into the MB entity, so detail resolves it by id, never by name.
3. **Add YouTube Music (and Amazon) to the content fan-out** — but only if their id is bridgeable; otherwise they inherit SoundCloud's name problem.
4. **Separate "coverage" from "authority."** MB as a *vote* (adds corroboration weight) rather than a *veto* (removes) — matching the "every provider is an equal source, MB is the only one that removes" doctrine that may itself be the thing to revisit.

Each of these is a *layer change*, not a patch. Decide here, on the map, before touching code.

---

## 6. Clean-slate redesign (the decision: rebuild, not patch)

> **Build status (2026-07-23):** Steps 1–5 **shipped and live on prod** behind `DISCOGRAPHY_V2=true` (+ `DETAIL_IDENTITY_FIRST`). The pure cores (`release_merge.go`, `release_keep.go`, `release_bucket.go`) are wired via `get_artist_content_v2.go`; 1393 backend tests green. This fixes F2/F3/F4 at the source — best-of metadata (year/track-count), corroboration-keep (no MB-veto), normalized buckets. The client needs **no** change for correctness: each backend response is now complete, so its dedup is no longer lossy. **Remaining (device-gated / conditional):** step 4 (search-time SoundCloud id acquisition — needs SC to co-identify in search) and step 6 (client cutover to one call — removes the now-harmless F1 double-merge, needs an EAS build). Verify on Che before flipping the default.

Every fix this cycle was a band-aid on an algorithm that grew by accretion. The symptoms trace to **four architectural faults**, not four bugs:

- **F1 — Double merge.** The backend fans out and merges a full discography; then the client *re-fetches per provider and re-merges it*, discarding the backend's work. Two merge implementations, neither authoritative.
- **F2 — Replace, don't merge.** Both merges pick one variant of a release and drop the rest of its fields (backend `completenessOf` + consensus `add()`; client `dedupAlbumsByTitle`). A record with a cover but no year overwrites one with a year. **This is why "Fully Loaded" shows no year/tracks even though Deezer returns `release_date`.**
- **F3 — Completeness is blind.** `completenessOf` scores `ImageURL/ISRC/Duration/Album` — not year, track-count, or record_type. The tie-breaker can't even see the fields we care about.
- **F4 — Authority conflated with coverage.** MusicBrainz is both the contamination filter *and* a real-release purger. Loosening one role breaks the other (the `protectPrimaries` regression).

### Design principles for the rebuild

1. **One merge, one authority.** The backend produces the final, ranked, bucketed discography in a **single** call. The client renders it verbatim — no per-provider fetch, no client re-merge. (Deletes F1 and the client half of F2.)
2. **Merge is field-level best-of, never wholesale replace.** Clustering the same release across providers yields one record that takes the best of *every* field (cover, year, track-count, record_type, all identifiers, union of source ids). No variant is ever discarded. (Deletes F2, F3.)
3. **Identity-first, id-only fan-out.** Resolve the artist's full cross-provider identity once; query each provider by *its own id*. Contamination-proof by construction.
4. **Keep only on identity provenance.** *(Revised 2026-07-23 after the first V2 shipped — see the correction below.)* A release is kept iff at least one provider returned it when queried by the artist's **verified id** (own provider id or MBID). MusicBrainz becomes one contributing **vote** via best-of enrichment, never a veto. Name-only clusters — a same-name artist's release that only a by-name fetch surfaced — are dropped. Keeps ENCORE (Deezer own-id), drops namesakes. (Deletes F4.)

   > **Correction (why `HasStrongID` / name-corroboration are unsound).** The first V2 kept a release if it had a strong identifier (UPC/MBID) **or** ≥2 providers, in addition to own-id. Both leak contamination: a *namesake's* album carries its **own** valid MBID (so `HasStrongID` is true for the wrong artist), and two by-name providers can independently return the **same** wrong artist (so "≥2 providers" corroborates a namesake). Verified live: an iTunes by-name fetch for "che" returned `"still freestyle r.i.p moe 3 - Single"` by *"Lope fully loaded"* — a different artist, with a valid id. No per-release signal separates the right artist from a namesake; **only the provenance of the query does.** So the keep rule is exactly `IDVerified`. By-name groups are still merged (they enrich an id-verified cluster with a cover/date), but a cluster with **no** id-verified source is dropped.
5. **Close the id gap without name-guessing.** For providers MB never bridges (SoundCloud), acquire the id at *search* time — when a provider result provably co-identifies with the entity (shared ISRC/UPC, or exact title+artist under a non-ambiguous name) — and persist it to the `IdentityStore`. Detail then resolves it *by id*. Blind name-resolve is removed; if an id is truly unknown, the provider sits out rather than risk a wrong-artist match.
6. **Record-type is resolved, not trusted.** One normalizer folds every provider's signal (explicit single/EP flags → MB primary-type → iTunes suffix → 1-track⇒single) into a single record_type. Bucketing reads that. (Now viable because extraction is fixed.)

### Target pipeline (backend, one call)

```mermaid
flowchart TD
    IN["GET /artist/{id}/discography\n(seed provider+id, artistName)"] --> RES["1. Resolve identity\n{MBID, providerIDs} — id-only,\nids learned at search-time, no name-guess"]
    RES --> FAN["2. Fan out by id — each provider's\nreleases + top-tracks, concurrent"]
    FAN --> CLU["3. Cluster by release identity\nUPC/MBID → exact title+artist → norm-title"]
    CLU --> MER["4. Best-of merge each cluster\n(every field; union sources; resolve record_type)"]
    MER --> CONF["5. Confidence filter\nkeep corroborated / identifier-backed / own-id\ndrop single-source name-only namesakes"]
    CONF --> NORM["6. Normalize year + bucket (album/single/EP)"]
    NORM --> SORT["7. Sort newest-first"]
    SORT --> OUT["one merged discography → client renders verbatim"]
```

### What gets deleted

- Client `useArtistContent` per-provider fan-out (`getArtistAlbums('deezer'|'soundcloud'|'itunes')` × N) and `dedupAlbumsByTitle` / `backfillAlbumArt` / client sort — the backend owns all of it.
- `applyMBAuthority`'s veto role and the whole `protectPrimaries` concept.
- `completenessOf` as the merge arbiter (replaced by field-level best-of).
- Blind `resolveArtistIDByName` for SoundCloud (replaced by search-time id acquisition; corroboration-gated resolve only as an explicit fallback).

### Build sequence (strangler, not big-bang — matches ADR-0007)

The current path stays live behind `DETAIL_IDENTITY_FIRST`; build the new one behind a new flag and cut over when it's proven:

1. **Best-of merge core** (pure, unit-tested): `MergeReleases([]variants) → Release` — field-level, identifier-aware clustering. Golden tests incl. the ENCORE and Fully-Loaded cases.
2. **Confidence keep** (pure): `Keep(release) bool` from corroboration signals. Tests: ENCORE kept, namesake dropped.
3. **Record-type normalizer** (pure) + bucketer.
4. **Search-time id acquisition**: persist provider ids (esp. SoundCloud) into `IdentityStore` when co-identity is proven. This is what makes SC id-safe.
5. **New single endpoint** `GET /artist/{id}/discography` wiring 1–7; old per-kind endpoints stay until the client cuts over.
6. **Client**: replace the three-call fan-out + re-merge with one call that renders verbatim.
7. **Verify** on Che (ENCORE present, years/tracks/buckets correct, no namesakes), then flip the flag and delete the old layers.

Steps 1–3 are pure functions with no I/O — fast to write, exhaustively testable, and where all the correctness lives. That's the opposite of the band-aid pattern: the hard logic becomes small, tested, deletable-in-isolation units instead of accreted conditionals across the pipeline.
