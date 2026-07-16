# Go API

Hexagonal: dependencies point inward only (`adapters → service → domain`); ports in
`ports/`, wiring in `internal/app/`. Full layout: `<repo>/docs/architecture.md` (read
on demand). Bounded contexts carry their own nested `CLAUDE.md` (e.g.
`internal/discovery/CLAUDE.md`).

Go pattern vocabulary (index only — full entries under `~/.claude/lexicon/site/`, read on demand):

@~/.claude/lexicon/MANIFEST-go.md

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
