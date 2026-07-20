---
type: Index
title: Discovery subsystems
description: The discovery bounded context decomposed into its nine subsystems — search orchestration through ranking, enrichment, telemetry, and the offline eval harness.
tags: [index, discovery]
---

Discovery is the multi-provider search context (`services/go-api/internal/discovery/`, ~18K LOC — 62% of the backend). A query flows: scatter-gather fan-out → identity stamping → merge/dedup → ranking → (on detail-open only) enrichment. Everything rank-affecting is eval-gated.

## Query path (in flow order)

- [scatter-gather](scatter-gather.md) — the Service facade fanning a query across providers in parallel
- [identity-artwork](identity-artwork.md) — durable EntityIdentity bridge + identity-first artwork chain for same-name entities
- [merge-dedup](merge-dedup.md) — identity-tiered entity resolution, artist consensus, disambiguation
- [ranking](ranking.md) — parameter-free relevance scoring, eval-gated tail-demotion and cross-kind prominence, diversity reshaping
- [query-correction](query-correction.md) — "did you mean", query cleanup, phonetics, autocomplete
- [vocabulary](vocabulary.md) — learned term-frequency store backing correction and suggest

## Off the query path

- [enrichment](enrichment.md) — detail-screen MusicBrainz/Discogs/Last.fm/Deezer metadata + lyrics, never on the ranking path
- [cache-layer](cache-layer.md) — Redis read-through caches (no-op when Redis is absent); caches are app-wide, not per-user
- [telemetry](telemetry.md) — InteractionEvent pipeline + SatisfactionConsumer turning play/skip/completed into a ranking signal
- [eval-harness](eval-harness.md) — offline discoveryeval CLI measuring ranking/merge/diversity/coverage against committed baselines

The Mission Control operator console that displays discovery's telemetry is its own module — see [admin](../admin/index.md); discovery feeds it through consumer-defined seams and never imports it.
