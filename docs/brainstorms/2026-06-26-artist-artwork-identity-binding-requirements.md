---
date: 2026-06-26
topic: artist-artwork-identity-binding
---

# Artist Artwork — Identity Binding

## Summary

Rebuild **artist** artwork resolution so the image is keyed to the *identity the pipeline already proves* (the Discogs/Spotify/Deezer/Wikidata links MusicBrainz supplies per artist) rather than to the artist's *name* — so a discovery card shows the right face, and name-search resolvers are never allowed to guess when a proven identity exists.

---

## Problem Frame

On the discovery results page, the artwork on a card is what tells a user *who* a result is — it drives whether they tap an artist, album, or single. For ambiguous same-name artists the card shows the **wrong person's face**, consistently. "Che" is the canonical case: the search returns one merged artist entity for the Atlanta rapper, but its image is a different Che entirely, every time.

The cause is identity-blindness. Artist artwork is resolved by **name**: the merge proves a specific identity and then a separate enrichment pass throws that away and queries providers for the string "Che", taking whichever same-name artist answers first. The resolvers accept an `mbid` parameter and ignore it; they never see the cross-provider identity links the merge already computed. A bare-name resolver (TheAudioDB) sits high in the chain and supplies a wrong-Che thumbnail. The wrong result is then cached in Redis keyed by name, so it persists across requests regardless of any chain change.

This has been partially designed three times before (`docs/brainstorms/artist-detail-quality-requirements.md`, `docs/brainstorms/2026-06-19-artist-identity-v2-requirements.md`, `docs/brainstorms/2026-06-18-discovery-detail-residuals.md`), but the discovery rebuild (ADR-0007 strangler) regressed several of those fixes — the MBID-first ordering is incomplete, name-only resolvers were never demoted to last, and the album-art fallback is gone. The detail screen compounds it by re-resolving artwork by name a *second* time on open, overwriting the card image and producing a visible flicker.

A spike confirmed the fix is reachable from existing data: the merge already stamps the correct Che's full identity graph (`discogs:15250716 spotify:5A7T1LAGJg5NXySBoIKUmF deezer:234701081 wikidata:Q134697068`), and the bridged Discogs ID resolves to "Che (38)" with a curated primary photo. The pipeline holds the identity and a Discogs token and uses neither.

---

## Requirements

**Identity-bound resolution (backend)**
- R1. Artist artwork resolution consumes the proven cross-provider identity links the merge already computes for the entity (MBID plus the bridged Discogs/Spotify/Deezer/Wikidata IDs), not only the display name.
- R2. When a proven identity link exists for a provider, that provider fetches the **exact** entity by its ID — never a name search. (e.g. the bridged Discogs artist ID is fetched directly.)
- R3. Resolution follows a strict ladder, stopping at the first hit: (1) identity-keyed photo (MBID-based sources and bridged provider IDs), (2) disambiguation-augmented name search for providers with no cross-linkable ID, (3) the artist's own top-release cover, (4) a deliberate placeholder. A bare name-search photo is **never** used when a proven identity exists for the entity.
- R4. Name-only resolvers (TheAudioDB, and bare name search on Deezer/iTunes/SoundCloud) are ordered below every identity-based resolver and are skipped entirely when the entity carries identity IDs.
- R5. A search provider's own valid (non-placeholder) artist image is preferred and not overwritten by a lower-confidence name-resolved image. Artists stop unconditionally re-resolving when they already hold a usable photo.

**Caching**
- R6. The artwork cache is keyed by a stable identity signature, not by name+subtitle, so two distinct same-name artists can never collide on a cache entry and a correct image is never shadowed by a same-name one.
- R7. Existing wrong/stale artwork cache entries are invalidated so the fix takes effect immediately rather than only after TTL expiry.

**Detail-screen coupling (frontend)**
- R8. The detail-screen hero locks to the artwork carried by the tapped result and does not re-resolve artwork by name on open — eliminating the flicker and the second wrong guess.

**Quality gate**
- R9. The change is validated against the discovery eval harness and the canonical "Che" spot-check before it is considered done.

---

## Acceptance Examples

- AE1. **Covers R1, R2, R4, R9.** Search "Che". The artist card shows the Atlanta rapper's real face sourced from his bridged identity (Discogs/Spotify), not TheAudioDB's wrong-Che thumbnail.
- AE2. **Covers R3.** Given an artist with a proven identity but no photo on any identity-based source, the card shows that artist's own top-release cover — never a name-searched face of a different person.
- AE3. **Covers R5.** Given an artist whose search provider supplied a real (non-placeholder) photo, that photo is shown and not overwritten by a name-resolved one.
- AE4. **Covers R6.** Given two distinct artists who share a name, each shows its own correct (or own-release) image; one never displays the other's.
- AE5. **Covers R8.** Tapping an artist on discovery opens a detail screen whose hero is the exact image shown on the card — stable, with no flicker or mid-load switch.

---

## Success Criteria

- Searching ambiguous artists (Che and the same-name long tail) shows the right face, or an honest own-release-cover fallback — never a different person's photo. Users can recognise and tap results with confidence.
- A downstream implementer can tell from logs which identity source produced each artist image, and the eval harness has a spot-check that fails if "Che" regresses to a wrong-artist image.

---

## Scope Boundaries

- Track and album artwork are unchanged — they already use the matched provider's own cover correctly.
- No changes to discovery ranking or result ordering. Artwork only.
- Not collapsing the wall of same-name artist results in the search list (a separate prior concern).
- No new external provider integrations beyond reaching the identity links already computed. Full Spotify-API integration is excluded (2026 API lockdown); *using* the Spotify ID we already hold to fetch an image is a deferred option, not part of this scope.

---

## Key Decisions

- **Identity over name.** Artwork is keyed to the proven identity graph, not the name string. Same-name artists are different people; only identity disambiguates them, and the pipeline already computes that identity — it was simply being discarded.
- **Never a wrong face.** When identity cannot yield a photo, fall back to the artist's own release cover, never a name-guessed face. A wrong face actively misleads tapping; an own-release cover is always truthful.
- **Bind at resolution time, cache by identity.** Capturing the image against the proven identity (and caching by identity, not name) structurally removes the sticky-wrong-cache failure mode.

---

## Dependencies / Assumptions

- MusicBrainz url-relations populate the cross-provider identity bridge. Verified present and complete for "Che"; coverage varies by artist and sets how often the own-release-cover fallback (R3.3) fires.
- A Discogs token is configured and Discogs exposes per-same-name-artist entries with images. Verified (artist `15250716` = "Che (38)", primary image present).
- For artists MusicBrainz has not linked anywhere, identity resolution yields nothing and the own-release-cover fallback carries them — so coverage of the bridge is not a correctness risk, only a "photo vs. cover" quality difference.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R3][Needs research] For what fraction of artists does MusicBrainz provide at least one usable identity link or photo? Determines how often the own-release-cover fallback fires versus a real photo.
- [Affects R3][Technical] Whether to fetch an image via the Spotify ID we already hold through an open endpoint (oEmbed/embed) without the locked-down API.
- [Affects R6, R7][Technical] The exact identity signature used for the cache key, and the mechanism for the one-time invalidation of stale entries.
