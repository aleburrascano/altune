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
├── docs/                # decisions and history (see docs/README.md)
├── scripts/             # the pre-commit staleness checks + AltStore source update
└── .github/workflows/   # all CI — a nested .github/ elsewhere is never read
```

Two conventions carry most of the weight:

- **Nested `CLAUDE.md` files are the file maps.** Every directory worth knowing about has one, listing its files, tests, and the rules in force there. Read the relevant one before structural work; a pre-commit hook blocks commits that let one go stale.
- **`apps/mobile/` and `services/go-api/` are comment-free.** If code needs explaining, it gets renamed or split — durable rationale goes in the nested `CLAUDE.md` files.

## Conventions

- Commits: [Conventional Commits](https://www.conventionalcommits.org/), scopes in `commitlint.config.js`, template in `.gitmessage`
- Domain vocabulary is fixed by [`docs/ubiquitous-language.md`](docs/ubiquitous-language.md) — "Song" is banned; the noun is `Track`
- Features are planned and tracked as GitHub issues — one ticket per unit of work, closed by its PR
