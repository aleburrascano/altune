# Altune

Self-hosted music manager: discover music across providers, build a library you own, stream it. Solo + Claude, production-grade.

## Stack

- **Mobile:** Expo (React Native + TypeScript) — [`client/`](client/)
- **API:** Go, hexagonal modular monolith — [`services/go-api/`](services/go-api/)
- **Data:** Supabase Postgres (session-mode pooler) + Redis
- **Prod:** single OCI VM, Caddy in front, blue-green deploys from `main`

## Getting started

```bash
npm run dev:up                  # Postgres + Redis for local work
cd services/go-api
cp .env.example .env.development
go build -o ./tmp/api.exe ./cmd/api && ./tmp/api.exe

cd client && npm start
```

## Layout

```
.
├── client/              # Expo app; vertical slices under src/features/
├── services/go-api/     # Go API; hexagonal, deploy/ holds everything Docker
├── services/overseer/   # read-only ops dashboard over go-api's /observe/*, shipped with go-api
├── services/supabase/   # Supabase auth config per tier and the script that checks it for drift
├── docs/                # durable docs: conventions, features, providers, diagrams, glossary (see docs/README.md)
├── scripts/             # repo tooling: precheck, violation and cycle checks, e2e stack, AltStore source update
├── scripts/ci/          # every script the CI workflows run
├── .gitea/workflows/    # all CI and deploys, on the self-hosted Gitea
└── .github/workflows/   # the iOS release only (needs a macOS runner); GitHub is otherwise a read mirror of main
```

**`client/` and `services/go-api/` are comment-free.** If code needs explaining, it gets renamed or split — durable rationale goes in `docs/`.

## Conventions

- PR titles: `type(scope): subject`, checked by `scripts/ci/pr-shape.sh`
- Go formatting: `bash services/go-api/scripts/guardrails.sh fmt` is the formatter of record — golangci-lint's bundled gofumpt, the exact formatter the CI gate enforces, across both Go modules. **Never run standalone `gofumpt -w`:** it splits stdlib from local imports while the gate wants a single alphabetical group (local `altune/...` first), so its output is rejected by CI.
- Domain vocabulary is fixed by [`docs/ubiquitous-language.md`](docs/ubiquitous-language.md) — "Song" is banned; the noun is `Track`
- Features are planned and tracked as Gitea issues — one ticket per unit of work, closed by its PR
