# Provider maximization

One markdown file per external provider, documenting **everything we can extract from it** —
not just what we currently wire. The goal: for each provider, know its full surface so we can
decide, deliberately, which capabilities to use and in what order.

## Why this folder exists

Discovery coverage and the owned-library product both live or die on how much we can pull from
each provider. Most providers expose far more than their official/public API advertises — the
internal JSON API their own website calls is usually richer, faster, and free. These docs capture
that surface, verified by **direct live probing** (per the project rule: check provider APIs
directly, don't answer from memory), so the knowledge compounds instead of being re-discovered
every session.

## The access doctrine (tiered)

When maximizing a provider, prefer access methods in this order:

1. **Internal JSON API** — the undocumented backend the provider's own website calls
   (e.g. `api-v2.soundcloud.com`, Deezer `gw-light.php`, Spotify's web-player API). Richest,
   fastest, and what the site itself trusts. Usually gated by a public key/token the site ships
   to every browser — bootstrap it, cache it, self-heal on rotation.
2. **Official / public API** — the sanctioned, documented endpoint (e.g. `api.deezer.com`,
   `itunes.apple.com/search`). Stable but often thinner, rate-limited, or partly paywalled.
3. **HTML scraping** — *last resort only*, when neither JSON API exists or both gate the data.
   The website is almost always rendered *from* the internal API, so scraping yields strictly
   **less** than tier 1. Fast scraping tooling exists, but reach for it only when there is no
   JSON backend to talk to.

> **Rule of thumb:** if a provider has an internal JSON API, scraping is the wrong tool. Find the
> API the site calls before writing a single CSS selector.

The doctrine generalizes across providers; the **key/token bootstrap is bespoke per provider**.

## ToS posture

Internal-API access is reverse-engineered and against most providers' ToS. Accepted for this
project's **self-hosted, personal/family use** — named explicitly, not hidden. Public-only reach:
truly private/unlisted content never surfaces.

## Per-provider status

| Provider | Doc | Audited | Notes |
|---|---|---|---|
| SoundCloud | [soundcloud.md](soundcloud.md) | ✅ live-probed 2026-06-21 | **4/6 capabilities built** (search all-kinds, artwork, discography); related-tracks + acquisition are TODO (need specs — see §8) |
| Deezer | [deezer.md](deezer.md) | ✅ live-probed 2026-06-22 | **Search + charts + content + ISRC + 1000px artwork + detail-open enrichment built.** The **popularity primary** (`nb_fan`/`rank`). Detail enrichment (caps 7–8) live: track `bpm`/explicit + album `label`/`genres` via `DeezerEnricher` → `GET /discovery/enrichment/deezer` → mobile `DeezerEnrichmentSection` (public API, no key, display-only). Remaining: **lyrics — synced + plain + writers** via the anonymous-JWT `pipe.deezer.com` GraphQL (the one axis MB/Discogs/Last.fm lack; legacy `gw-light song.getLyrics` is auth-gated) — a separate feature; `pipe` is reverse-engineered (grey, self-host) |
| iTunes / Apple Music | _stub_ | ⬜ | Public search used; previews + richer Apple Music API un-audited |
| YouTube Music | _stub_ | ⬜ | Via `ytmusic` lib; full internal surface un-audited |
| MusicBrainz | [musicbrainz.md](musicbrainz.md) | ✅ live-probed 2026-06-22 / 06-21 | **Fully maximized.** Search + identity/consensus + `inc=` enrichment + CAA artwork; identity-merge (cap 4) & search-list artwork (cap 5) ✅ eval-passed (top-3 99.4%, ADR-0011); Fanart.tv (cap 6) ✅ live-verified. Only the cold-entity background MBID-warm worker remains deferred |
| Last.fm | [lastfm.md](lastfm.md) | ✅ live-probed 2026-06-22 | **Search + charts + artist-content + detail-open `*.getInfo` enrichment built (cap 3 + cap-4 similar artists).** Listen-based **popularity** (`listeners`/`playcount` — the axis MB+Discogs lack), weighted **tags**, **bio**, **similar artists**, + the **MBID bridge**, via `LastFmEnricher` → `GET /discovery/enrichment/lastfm` → mobile `LastFmEnrichmentSection`. API key, ~5 req/sec, display-only. **Not an artwork source** (300px + placeholder artist images, verified). Remaining: similar-**tracks** rail (needs `/feature-spec`), tag-discovery (cap 5); popularity-into-rank eval-gated, Deezer stays primary |
| Discogs | [discogs.md](discogs.md) | ✅ live-probed 2026-06-22 | **Fully maximized (caps 1–7 built).** Artwork fallback (≤600px) + artist identity consensus + **detail-open album enrichment** (credits/personnel, styles, label/catalog, formats/companies, community rating) + **artist enrichment** (bio, name history, group/member links, external links) via `DiscogsEnricher` → `GET /discovery/enrichment/discogs[/artist]` → mobile `DiscogsEnrichmentSection` / `DiscogsArtistSection`. Token API, 60 req/min. No ISRC/MBID — resolves via structured artist+title / artist-name search. Optional refinements only: tighter matching via the MB-bridge discogs id; styles/community into rank (eval-gated) |

## How to audit a provider

Follow [`_template.md`](_template.md). Every claim about an endpoint or field must be backed by a
**live probe this session** (status code + real field dump), not recollection. Mark anything
unverified as `[INFERRED]`.
