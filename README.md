# Altune

Self-hosted music manager: discover music across providers, build a library you own, stream it. Solo + Claude, production-grade.

## Stack

- **Mobile:** Expo (React Native + TypeScript) — [`apps/mobile/`](apps/mobile/)
- **API:** Go, hexagonal modular monolith — [`services/go-api/`](services/go-api/)
- **Data:** Supabase Postgres (session-mode pooler) + Redis
- **Prod:** single OCI VM, Caddy in front, blue-green deploys from `main`

## Getting started

```bash
npm run dev:up                  # Postgres + Redis for local work
cd services/go-api
cp .env.example .env.development
go build -o ./tmp/api.exe ./cmd/api && ./tmp/api.exe

cd apps/mobile && npm start
```

## Layout

```
.
├── CLAUDE.md            # project constitution — rules that are always in force
├── apps/mobile/         # Expo app; vertical slices under src/features/
├── services/go-api/     # Go API; hexagonal, deploy/ holds everything Docker
├── okf/                 # knowledge bundle — why the code is the way it is
├── docs/                # decisions and history (see docs/README.md)
├── scripts/             # the pre-commit staleness checks + AltStore source update
└── .github/workflows/   # all CI — a nested .github/ elsewhere is never read
```

Two conventions carry most of the weight:

- **`CLAUDE.md` instructs, `okf/` explains.** Every directory worth knowing about has a nested `CLAUDE.md` acting as its file map; the reasoning behind the rules lives in `okf/`, indexed from [`okf/index.md`](okf/index.md). Pre-commit hooks block commits that let either go stale.
- **`apps/mobile/` and `services/go-api/` are comment-free.** If code needs explaining, it gets renamed or split — durable rationale goes in the two files above.

## Conventions

- Commits: [Conventional Commits](https://www.conventionalcommits.org/), scopes in `commitlint.config.js`, template in `.gitmessage`
- Domain vocabulary is fixed by [`docs/ubiquitous-language.md`](docs/ubiquitous-language.md) — "Song" is banned; the noun is `Track`
- Features follow spec → plan → TDD → verify → review ([`docs/workflows/new-feature.md`](docs/workflows/new-feature.md))
- Architecture decisions land in [`docs/adr/`](docs/adr/)
