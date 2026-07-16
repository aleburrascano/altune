---
type: Index
title: Provider integrations
description: External music-metadata providers — what each uniquely contributes, its auth/rate posture, and where its adapter lives.
tags: [index, providers, discovery]
---

Every provider adapter lives in `services/go-api/internal/discovery/adapters/providers/`. Each concept records the provider's one non-duplicative contribution — the axis no other provider covers.

- [deezer](deezer.md) — primary search/artwork/charts/ISRC surface; internal pipe GraphQL adds detail metadata and time-synced lyrics
- [musicbrainz](musicbrainz.md) — the identity hub: mints MBIDs unlocking HD artwork and bridging to every other provider's ids
- [discogs](discogs.md) — deepest structured metadata: credits, styles, label/catalog, community demand, artist bio
- [lastfm](lastfm.md) — listen-based popularity, relatedness graph, folksonomy tags — the listening-behavior axis
- [soundcloud](soundcloud.md) — reverse-engineered api-v2: UGC search/discography/related, yt-dlp fallback
- [youtube-music](youtube-music.md) — keyless internal-API search; unique contribution is hi-res artist artwork
- [itunes](itunes.md) — keyless mainstream search/identity; unique contribution is 1500px hero artwork above Cover Art Archive's ceiling
- [genius](genius.md) — config-gated, identity-blind artwork resolver (no lyrics, despite the name)
- [artwork-chain](artwork-chain.md) — the ordered artwork-only fallback chain (identity-keyed first, then name-search) when Deezer artwork is missing
