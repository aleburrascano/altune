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

```bash
cd services/go-api

# Build
go build -o ./tmp/api.exe ./cmd/api

# Run locally (needs .env with DB/Redis)
./tmp/api.exe          # or `air` for hot reload

# Test + vet
go test ./... -count=1
go vet ./...
```

Code changes don't take effect until you rebuild and restart the process.

## Knowledge base

`okf/backend/index.md` indexes the curated concept docs for every context and subsystem — read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
