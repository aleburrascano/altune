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
├── apps/mobile/         # Expo app; vertical slices under src/features/
├── services/go-api/     # Go API; hexagonal, deploy/ holds everything Docker
├── docs/                # decisions and history (see docs/README.md)
├── scripts/             # the pre-commit staleness checks + AltStore source update
└── .github/workflows/   # all CI — a nested .github/ elsewhere is never read
```

**`apps/mobile/` and `services/go-api/` are comment-free.** If code needs explaining, it gets renamed or split — durable rationale goes in `docs/`.

## Conventions

- Commits: [Conventional Commits](https://www.conventionalcommits.org/), scopes in `commitlint.config.js`, template in `.gitmessage`
- Go formatting: `bash services/go-api/scripts/guardrails.sh fmt` is the formatter of record — golangci-lint's bundled gofumpt, the exact formatter the CI gate enforces, across both Go modules. **Never run standalone `gofumpt -w`:** it splits stdlib from local imports while the gate wants a single alphabetical group (local `altune/...` first), so its output is rejected by CI.
- Domain vocabulary is fixed by [`docs/ubiquitous-language.md`](docs/ubiquitous-language.md) — "Song" is banned; the noun is `Track`
- Features are planned and tracked as GitHub issues — one ticket per unit of work, closed by its PR
