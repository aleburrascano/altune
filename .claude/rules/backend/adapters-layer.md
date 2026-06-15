---
paths:
  - "services/go-api/**/adapters/**"
---

# Adapters layer — drivers and driven

Adapters are the **outer ring**. They translate between the framework world (HTTP, SQL, message brokers, external APIs) and the application layer's ports.

## Two flavors

- **Inbound (driving) adapters** — `adapters/handler/`. The outside calls these. They turn external requests into use case calls.
  - HTTP handlers (chi routers)
  - CLI commands (if/when added)
  - Queue consumers, scheduled jobs
- **Outbound (driven) adapters** — `adapters/persistence/`, `adapters/storage/`, `adapters/providers/`, `adapters/cache/`. The application calls these through ports.
  - Persistence: repository implementations (database/sql, sqlx)
  - Storage: file/object storage (S3, OCI, filesystem)
  - Providers: third-party API clients (SoundCloud, iTunes, MusicBrainz, etc.)
  - Cache: Redis, in-memory caches

See `[vault: wiki/concepts/Hexagonal Architecture.md]`.

## Imports allowed

- `ports/` interfaces
- `domain/` types (to construct domain objects from persistence data)
- `service/` (only for handler → service wiring)
- Framework code: `chi`, `database/sql`, `net/http`, `encoding/json`, Redis clients, etc.
- `internal/shared/` for cross-cutting utilities (`httputil`, `config`, `logging`)

## Imports FORBIDDEN

- Other adapters cross-importing — adapters are siblings. They coordinate through the application layer, not directly.

## Inbound HTTP handler shape

```go
type PlaylistHandler struct {
    svc *service.PlaylistService
}

func NewPlaylistHandler(svc *service.PlaylistService) *PlaylistHandler {
    return &PlaylistHandler{svc: svc}
}

func (h *PlaylistHandler) Routes() chi.Router {
    r := chi.NewRouter()
    r.Post("/", h.handleCreate)
    r.Get("/", h.handleList)
    r.Delete("/{playlistId}", h.handleDelete)
    return r
}

func (h *PlaylistHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
    userId := auth.UserFromContext(r.Context())
    var body CreatePlaylistRequest
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    playlist, err := h.svc.Create(r.Context(), userId, body.Name)
    if err != nil {
        httputil.HandleServiceError(w, err)
        return
    }
    httputil.WriteJSON(w, http.StatusCreated, toPlaylistResponse(playlist))
}
```

The handler is a **thin shell**: parse request → call service → serialize response. No business logic.

## Outbound persistence adapter shape

Repository implements a port interface. Owns the SQL ↔ domain mapping. Domain objects never see database models.

## Testing

- Inbound handlers tested with `httptest.NewServer` against in-memory service implementations (the service is the seam).
- Outbound persistence adapters tested against real databases (integration tests behind `//go:build integration`).
- Outbound external API adapters tested with HTTP mocking or recorded fixtures.

## Anti-patterns

- Business logic in handlers (validation that the domain should own, conditionals on domain state).
- Repositories returning framework types (database rows, JSON maps) instead of domain types.
- Bypassing the service layer ("just call the repo from the handler for this quick fix"). **Always go through the service** — the service is where transactions and invariants are coordinated.
- Adapters constructing domain objects with invalid state. The domain enforces invariants; the adapter is just a translator.
