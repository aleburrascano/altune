# Featured artists — spec

**Status:** Accepted (brainstormed 2026-07-05)
**Scope:** Source, persist, display, tappable, and browsable featured-artist ("feat.") data across the app.

## Problem

A track today is a single flat `Artist` string. "Featured artists" (the `feat. X, Y` guest credits) are:

- **Not produced** by the Go backend — `featured_artists` is a documented-but-empty `SearchResult.Extras` key (`services/go-api/internal/discovery/domain/types.go:276`); no code writes it.
- **Not persisted** — the `tracks` table is all flat scalars; no featured/contributor column.
- **Not really displayed** — the only mechanism that fires today is a client-side **regex** on the title (`apps/mobile/src/features/detail/extras.ts` `extractFeaturedFromText`). The richer paths described in `apps/mobile/src/features/detail/CLAUDE.md` (a "three-tier" system: Deezer contributors → MB `artist-credit[1:]` → regex) are **stale**: tier 1 (`_enrich_contributors`) only existed in the retired Python backend, and tier 2 (`useAlbumTracks._mergeFeaturing`) reads a key the backend never populates, so it is a dead no-op. Only the regex tier runs.

Title-regex is unreliable (misses anything not literally in the title; mis-parses punctuation). We want **best-quality, structured** featured-artist data, sourced from the discovery pipeline we already trust.

## Goals

1. **Source** featured artists from structured provider data — MusicBrainz `artist-credit` (name + MBID + joinphrase) merged with Deezer `/track/{id}` `contributors` (explicit `Featured` role + Deezer artist id) — via one resolver **inside the discovery context**, reusing existing enrichers + Redis cache.
2. **Persist** featured artists (with canonical ids) on saved tracks.
3. **Display** `feat. X, Y` everywhere a track's title/artist renders: MiniPlayer, FullPlayer, QueueSheet, LibraryRow, DiscoverRow (detail already has the UI).
4. **Tappable** — featured names navigate to the artist (lateral nav, already exists on detail).
5. **Browsable** — "everything featuring X" query + library screen.

## Non-goals

- Producer/engineer credits (Discogs album personnel) — out of scope; `role` field reserved but only `"featured"` is populated.
- Full `Artist` aggregate — featured artists are value objects keyed on id, not a new aggregate root.
- Re-running discovery for every save — new saves reuse the already-browsed result; only backfill spends provider calls.

## Design

### Domain model — `FeaturedArtist` value object `[INFERRED — vault unavailable this session; see ADR-00XX]`

Immutable, equality by attributes, no lifecycle:

```
FeaturedArtist {
  name     string    // display, e.g. "Kendrick Lamar"
  mbid     *string   // MusicBrainz id — canonical grouping key when present
  deezerId *int64    // Deezer artist id — fallback key + tappable target
  role     string    // "featured" (reserved for future roles)
}
```

Lives in two places:
- **Discovery**: `SearchResult.Extras["featured_artists"]` as `[]map[string]any` (wire) — the key graduates from placeholder to populated.
- **Catalog**: an ordered collection on the `Track` aggregate (persisted).

**Grouping key** (for identity + "featuring X"): `mbid` if present, else `deezerId`, else normalized `name`.

### Producer + resolver (discovery)

- Decode Deezer `contributors` (add `Contributors []deezerContributor` with `{id, name, role}` to the track detail fetch in `deezer_enrichment.go`).
- Surface MB `artist-credit[1..]` + `joinphrase` (add `JoinPhrase string` to `mbArtistRef`; read indices ≥1).
- `FeaturedArtistResolver` service merges both — **MB-primary, Deezer fills gaps** (matches pipeline MB-authority). Union of entities keyed by grouping key; MB supplies MBID, Deezer supplies deezerId, matched by name-normalization + shared ids where available.
- Populate `extras["featured_artists"]` in the detail/track paths, mirroring the `collapsed_artists` producer pattern (`service/diversity.go:122`). Reaches the client for free (wire passes `Extras` wholesale).

### Persistence — migration `010_track_featured_artists.sql`

```sql
CREATE TABLE featured_artists (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id      UUID NOT NULL,
  mbid         TEXT,
  deezer_id    BIGINT,
  name         TEXT NOT NULL,
  norm_name    TEXT NOT NULL,
  identity_key TEXT GENERATED ALWAYS AS (COALESCE(mbid, 'dz:'||deezer_id::text, 'name:'||norm_name)) STORED,
  UNIQUE (user_id, identity_key)
);
CREATE TABLE track_featured_artists (
  track_id           UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  featured_artist_id UUID NOT NULL REFERENCES featured_artists(id),
  position           INT  NOT NULL,
  PRIMARY KEY (track_id, featured_artist_id)
);
CREATE INDEX idx_tfa_featured_artist ON track_featured_artists(featured_artist_id);
```

### Cross-context seam (hexagonal)

- Resolver stays in **discovery**.
- **Catalog** defines port `FeaturedArtistResolver` + a **bridge adapter** calling the discovery service — mirrors `playback/adapters/catalogbridge/now_playing_reader.go`. No `catalog → discovery` import.
- New saves: client already holds browsed `featured_artists` → threaded through the save contract. No resolver call.
- Backfill: admin-triggered batch invokes the resolver by ISRC.

### Save-path threading

Extend `CreateTrackRequest` (`track_handler.go`), `AddTrackInput` (`add_track.go`), `TrackResponse` + `CreateTrackRequest` (mobile `types.ts`), and `toCreateTrackRequest` (`save-cache.ts`) to carry `featured_artists`. Persist via the repo (new INSERT into join tables inside the add-track transaction).

### Backfill (admin-triggered)

Admin endpoint enqueues a batch job over the user's tracks that have an ISRC; for each, call the resolver, upsert `featured_artists` + `track_featured_artists`. Idempotent (dedup on `identity_key`). Cache-warmed via existing Redis enrichment cache.

### Display (mobile)

Add `featuredArtists` to `TrackResponse`, `PlaybackTrack`, and `toPlaybackTrack`. Render `feat. X, Y`:
- `MiniPlayer.tsx`, `FullPlayer.tsx`, `QueueSheet.tsx`, `LibraryRow.tsx`, `DiscoverRow.tsx`.
- Detail already renders it — swap the regex crutch for the now-populated data (regex becomes fallback only).
- A shared helper `formatFeaturing(featured)` in `shared/lib` (2+ consumers → qualifies for shared).

### Tappable + browse

- Featured names tappable → `useLateralNav` to artist detail (by name; mbid/deezerId available for precision later).
- Browse: `GET /v1/tracks?featuring={featured_artist_id}` (catalog query + repo method) → a filtered `SongsList` screen reached from a "N in your library" affordance.

### Conflict rule

MB and Deezer disagree on the featured set → **MB-primary, Deezer fills gaps** (precision over recall; MB is the pipeline's identity authority).

## Slices (build order)

1. Discovery producer (Deezer contributors + MB artist-credit[1:] + resolver + populate extras) — fixes detail screen immediately.
2. Domain `FeaturedArtist` + `Track` field + migration 010 + repo.
3. Save-path threading.
4. Backfill (admin) + catalog→discovery bridge.
5. Mobile display (5 surfaces).
6. Tappable.
7. "Featuring X" browse (endpoint + screen).

## Verification

TDD per slice (sacred-tests rule). Key cases:
- Resolver merge: MB-only, Deezer-only, both-agree, both-conflict (MB wins), neither.
- Migration up/down; repo round-trip (upsert dedup on identity_key; ordering by position).
- Save threading persists featured artists; dedup hit path.
- Backfill idempotency (re-run yields no dup rows).
- Browse query returns correct tracks; per-user scoped.
- Detail regression: regex is fallback-only once data is present.

## Ubiquitous language

Add `FeaturedArtist`, `FeaturedArtistResolver`; note `featured_artists` extras key graduates to populated. Correct the stale three-tier claim in `apps/mobile/src/features/detail/CLAUDE.md`.

## ADR

`docs/adr/` — "Featured-artist identity & storage": value-object + normalized join keyed on MBID/Deezer-id; relationship to (and separation from) the discovery `entity_identity` bridge. Flagged for vault review (MCP unavailable at design time).
