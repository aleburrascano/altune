# Go API

Hexagonal: dependencies point inward only (`adapters → service → domain`); ports in
`ports/`, wiring in `internal/app/`. Full layout: `okf/backend/index.md` (read
on demand). Bounded contexts carry their own nested `CLAUDE.md` (e.g.
`internal/discovery/CLAUDE.md`).

Go pattern vocabulary: **Read `~/.claude/lexicon/MANIFEST-go.md` before proposing
or rejecting any abstraction** (an `@`-import here does not expand — nested
CLAUDE.md files load on demand, imports only expand at launch). Full entries under
`~/.claude/lexicon/site/{path}/index.html` — Grep an entry for `Avoid|Cost` and
quote its cost line when tradeoffs matter; never read a whole entry (~40k chars).

Everything about running this service — dev and prod — lives in `deploy/`: `compose.dev.yml` (Postgres + Redis for local work), `compose.prod.yml`, `Dockerfile`, `Caddyfile`, `caddy/`, and the deploy scripts. The module root holds Go code and its lint/env config, nothing else.

```bash
cd services/go-api

# Build
go build -o ./tmp/api.exe ./cmd/api

# Run locally (needs .env.development with DB/Redis)
./tmp/api.exe          # or `air` for hot reload

# Local infra (or `npm run dev:up` / `dev:down` / `dev:reset` from the repo root)
docker compose -f deploy/compose.dev.yml up -d

# Test + vet
go test ./... -count=1
go vet ./...

# Import-direction lint (what fails CI when the layering erodes)
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0 run
```

`.golangci.yml` enforces the import-direction rules that `.claude/rules/backend/domain-layer.md` and `docs/architecture.md` state as convention: domain purity (the inner ring may import only stdlib, sibling domain packages, `internal/shared` root value objects and `textnorm`), catalog reaching discovery only through `adapters/discoverybridge`, and `internal/shared` never importing a feature, the composition root, or auth. Each `deny` carries its own `desc:` naming the rule it protects.

## Deploy

`deploy/blue-green.sh` (helpers in `deploy/lib.sh`) is the only supported deploy path; `deploy/rollback.sh` reverses it. `deploy/blue-green_test.sh` runs both against stubbed `docker`/`curl` and asserts each failure path — run it after touching either. `deploy/caddy/upstream.conf` names the live colour and is gitignored — never track it, the VM rewrites it every deploy.

- Register every periodic background loop through `App.whenLeader`, never `Start(ctx)` directly, and always before `startBackgroundWhenLeader` runs.
- Keep `deploy/lib.sh`'s `log` on stderr — `active_color` is read via command substitution.
- A migration must stay compatible with the previous version — both colours share one database during the swap.
- Keep `deploy/Caddyfile` a single `import` — the colour lives in `deploy/caddy/upstream.conf`.
- Keep `name:` pinned in both compose files — dropping it re-derives the project name from the directory and orphans every existing container and volume.
- Delete `LEGACY_UPSTREAM_FILE` from `deploy/lib.sh` once the VM has deployed at least once after the `deploy/` move.

## Comment policy

`services/go-api/` is **comment-free**, matching `apps/mobile/`. The code is the source of truth: if something needs explaining, rename it or split it out. Only compiler directives (`//go:build`, `//go:embed`) are allowed. Durable rationale — invariants, provider fragility, regression history, anything a name cannot hold — lives in the nested `CLAUDE.md` files and `okf/`.

Code changes don't take effect until you rebuild and restart the process.

## Knowledge base

`okf/backend/index.md` indexes the curated concept docs for every context and subsystem — read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
