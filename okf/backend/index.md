---
type: Index
title: Backend contexts & subsystems
description: The Go API's bounded contexts (one per hexagonal module in services/go-api/internal/) and cross-cutting subsystems.
tags: [index, go-api, hexagonal]
---

Each bounded context is a hexagon: `domain/` (pure core) → `ports/` (interfaces the core needs) → `service/` (use cases) → `adapters/` (HTTP in, DB/providers/cache out), wired in the composition root ([app-wiring](app-wiring.md)). Dependencies point inward; contexts talk to each other only through ports.

## Bounded contexts

- [catalog](catalog/index.md) — Track & Playlist aggregates: owned music metadata, dedup, playlist ordering, audio storage/streaming
- [acquisition](acquisition/index.md) — yt-dlp pipeline that finds, ranks, downloads, verifies, tags, and stores audio for a saved Track (has its own index)
- [discovery](discovery/index.md) — multi-provider search, merge, ranking, enrichment (the largest context; has its own index)
- [playback](playback.md) — server-side persistence of the client-owned playback Queue snapshot (resume-on-reopen)
- [auth](auth.md) — Supabase JWT verification middleware injecting the verified user id into context
- [admin](admin/index.md) — Mission Control: the single-operator observability console under /admin (deliberately not hexagonal)

## Cross-cutting

- [app-wiring](app-wiring.md) — the composition root: `app/app.go` and `search_wiring.go`, where the entire object graph is assembled
- [shared-infra](shared-infra.md) — config, DB/Redis pools, structured logging + ring buffer, HTTP trace record/replay, text normalization, `UserId`
