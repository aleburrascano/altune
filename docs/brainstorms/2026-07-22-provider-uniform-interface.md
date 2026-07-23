# Provider uniform interface — "stop excluding them"

**Date:** 2026-07-22
**Status:** direction, not yet fully built. Being built incrementally in the discovery detail path.
**Context:** discussion while fixing the artist-detail screen (Che): missing album years, same-name track/album bleed, and the feeling that providers are treated inconsistently.

## The frustration (valid)

Providers are handled differently across the pipeline: some are in search, some in
artist-content, some in the artwork chain; some are queried by id, some by name;
Spotify and Apple Music are absent from the content fan-out entirely. It reads as
fragmentation — "oh, Spotify doesn't do this, so we treat it differently," repeated
per provider — instead of one cohesive module where everything is treated the same.

## Two kinds of difference (only one is a real problem)

**Accidental differences — bad, fix them.** Apple Music is absent from the content
fan-out only because nobody has written its `GetArtistAlbums`/`GetArtistTopTracks`
yet. That is an unfinished adapter, not a principled exclusion. Same shape as the MB
`first-release-date` we were parsing into the struct and then never mapping onto the
album (fixed 2026-07-22). These are the fragmentation worth killing.

**Essential differences — imposed by reality, not chosen.** MusicBrainz has no cover
art. SoundCloud tracks no release dates. Last.fm's artist "id" is a name, ambiguous
by construction. Spotify's artist-content endpoints are a different, unbuilt set of
pathfinder queries. You cannot make these "the same" — asking a provider for data its
API doesn't expose just returns nothing.

## The principle: uniform interface, not uniform providers

"All in one" should mean **one uniform interface, each provider fills what it can** —
which is exactly what the ports already are (`SearchProvider`,
`ArtistContentProvider`, `ArtworkResolver`, `AlbumContentProvider`). The service
iterates the provider map uniformly; it does not branch on provider identity. The
identity-first detail merge (`fanOutByIdentity` + `Merge`, 2026-07-22) is this model:
fan out to every provider that can answer, merge best-of, no single provider is "the
authority" for the result.

So the fix for the fragmentation is **not more branching** — it is registering the
missing providers *through the same interface* so the special-casing disappears.
Apple/Spotify aren't "treated differently," they're just not registered for the
content capability yet. Register them and they flow through the identical path.

## The one thing NOT to unify: trust

Uniform **interface**: yes. Uniform **trust**: no. Letting every provider's data in
with equal weight reintroduces the exact noise the authority model filters — Last.fm's
credited-on graph (compilations, "various artists," bootlegs) and same-name-artist
bleed (the "Che" problem). Reputation/quality belongs in **how results merge**
(best-of field selection, corroboration weighting), never in a per-provider `if` that
gatekeeps whether a provider is allowed in. Encode "MB is authoritative for dates,
Deezer/Apple for covers" as a merge signal, not a name check.

## Roadmap (build order)

1. **[done] MB date fix** — map `first-release-date` onto MB albums. Example of using
   what a provider supplies, uniformly. Fills most missing years + restores chronology.
2. **[done] Identity-first detail** — the uniform fan-out + best-of merge for detail
   (behind `DETAIL_IDENTITY_FIRST`). See `okf/backend/app-wiring.md`.
3. **[next] Apple Music as a content provider** — implement `ArtistContentProvider` on
   the Apple adapter (it already carries date + cover in `mapAppleMusicAlbum`). Joins
   the same fan-out and merge; zero new special cases; biggest coverage win. Needs the
   Apple artist id reachable (via search source id and/or the identity xref).
4. **[later] Spotify as a content provider** — same idea, but its content endpoints are
   unbuilt and the API is fragile (TOTP/hash), so more work for less certainty.
5. **[ongoing] Remove accidental special-cases** — e.g. Last.fm album fetch is
   name-based (ambiguous) and dateless; either give it an identity-safe path or stop
   treating it as an album source. Prefer deleting a special case over adding one.

## Current shape (reference)

- Artist content providers today: Deezer, iTunes, SoundCloud, Last.fm (top-tracks by
  MBID). Wired in `internal/app/discovery_wiring.go` (`artistProviders`).
- Album completeness comes from the consensus fan-out (`BuildConsensusProviders`):
  Last.fm, MusicBrainz, iTunes, YouTube Music, SoundCloud, Discogs — by name, clustered
  by title, MB-authority validated.
- Search fan-out (`buildDiscoveryProviders`): Deezer, Apple Music, MusicBrainz, Last.fm,
  SoundCloud, YouTube Music, Amazon Music, Spotify.
- Note the asymmetry the roadmap closes: Apple Music and Spotify are in **search** but
  not in **content**. That gap is the accidental difference to remove.
