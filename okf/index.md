---
type: Index
title: Altune knowledge bundle
description: Root index of the OKF bundle — curated, non-derivable knowledge about the altune codebase. Start here, descend only into the branch you need.
tags: [index, okf]
---

Altune is a self-hosted music manager: an Expo (React Native + TypeScript) mobile app in `apps/mobile/` and a Go hexagonal modular monolith in `services/go-api/`. This bundle records what the code cannot say — why things are built this way, invariants, contracts, gotchas. The code itself is always the source of truth for what it does.

## Directories

- [backend/](backend/index.md) — the Go API's bounded contexts and subsystems (hexagonal modules under `services/go-api/internal/`)
- [mobile/](mobile/index.md) — the Expo app's features and shared subsystems (`apps/mobile/src/`)
- [data/](data/index.md) — Postgres tables: schema intent, migration history, ownership
- [providers/](providers/index.md) — external music-metadata provider integrations and what each one uniquely contributes
- [playbooks/](playbooks/index.md) — operational runbooks: local dev, CI/CD, production deployment

## Conventions

- One concept per file; the file path is the concept's identity.
- Frontmatter: `type`, `title`, `description`, `resource` (repo path(s) the concept describes), `tags`, `verified_commit` (the commit at which the body was last verified against the code — this replaces the spec's `timestamp` and powers the pre-commit staleness check).
- Cross-links are standard markdown links, relative to the linking file.
- Domain vocabulary comes from `docs/ubiquitous-language.md` — "Song" is banned; the noun is `Track`.
