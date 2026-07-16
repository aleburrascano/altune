---
type: Index
title: Database tables
description: Postgres tables — schema intent, migration history, and which bounded context owns each.
tags: [index, postgres]
---

Supabase Postgres in production. Each table is owned by exactly one bounded context; nothing crosses context boundaries at the SQL layer.

## Catalog

- [tracks](tracks-table.md) — the Track aggregate: saved audio recording + acquisition lifecycle
- [playlists](playlists-table.md) — `playlists` + `playlist_tracks`: the Playlist aggregate and ordered membership

## Discovery

- [discovery-events](discovery-events-table.md) — unified InteractionEvent envelope with search-attribution join key and idempotent two-tier delivery
- [discovery-metrics](discovery-metrics-table.md) — nightly rollup giving Mission Control restart-surviving quality-metric history
- [entity-identity](entity-identity-table.md) — durable cross-provider identity map (the reverse identity bridge)
- [search-history-clicks](search-history-clicks-table.md) — `discovery_search_history` + `discovery_search_clicks`: per-user history ring buffer and click tracking

## Playback

- [playback-queue-state](playback-queue-state-table.md) — one row per user: the persisted Queue snapshot for resume-on-reopen
