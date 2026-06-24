# Go API — how to run

Parent rules: `<repo>/CLAUDE.md`, `~/.claude/CLAUDE.md`. Bounded contexts carry
their own nested `CLAUDE.md` (e.g. `internal/discovery/CLAUDE.md`) — read the one
closest to the code you're editing.

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
