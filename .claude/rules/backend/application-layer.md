---
paths:
  - "services/go-api/**/service/**"
  - "services/go-api/**/ports/**"
---

# Application layer — use cases and ports

The application layer is **the orchestrator**. It defines use cases (in `service/`), holds the port interfaces (in `ports/`), and coordinates between domain operations and I/O.

## Imports allowed

- `domain/` (freely)
- `ports/` (freely — ports are part of this layer)
- Go standard library
- `internal/shared/` for cross-cutting value objects (`shared.UserId`)

## Imports FORBIDDEN

- `adapters/` (concrete implementations) — the application layer defines ports; adapters implement them
- Framework code: `chi`, `gorm`, `sqlx`, `redis`, `net/http`
- Any I/O library directly — I/O goes through ports

## What lives here

### Ports (`ports/`)

Interfaces that use cases call. Adapters in `adapters/` implement them.

```go
type PlaylistRepository interface {
    Create(ctx context.Context, playlist *domain.Playlist) error
    ListForUser(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error)
    GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error)
    Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (deleted bool, err error)
}
```

**Port discipline:**
- Port methods speak in **domain types**, not framework types. `GetByID(ctx, id domain.PlaylistId) (*domain.Playlist, error)`, not `GetByID(ctx, id string) (map[string]any, error)`.
- Keep ports small (Interface Segregation Principle). A port with 12 methods is usually 3 ports in a trench coat.
- `context.Context` is always the first parameter.

### Use cases / services (`service/`)

One struct per use case (or tight cluster of related use cases). Receives ports through constructor injection.

```go
type ListTracksService struct {
    trackRepo ports.TrackRepository
}

func NewListTracksService(trackRepo ports.TrackRepository) *ListTracksService {
    return &ListTracksService{trackRepo: trackRepo}
}

func (s *ListTracksService) Execute(ctx context.Context, userId shared.UserId, limit, offset int) (*ListTracksOutput, error) {
    tracks, total, err := s.trackRepo.ListForUser(ctx, userId, limit, offset)
    if err != nil {
        return nil, fmt.Errorf("list tracks: %w", err)
    }
    return &ListTracksOutput{Tracks: tracks, Total: total, HasMore: offset+len(tracks) < total}, nil
}
```

## Testing

- Use cases unit-tested with **in-memory adapter implementations** (fake repositories, recording event publishers).
- No database, no HTTP. Tests are fast (<1ms each).
- Test the **behavior** (input -> output + side effects on ports), not the implementation.

## Anti-patterns

- Use cases that import from `adapters/` — port discipline broken.
- Use cases that contain domain logic — push it into the aggregate.
- Use cases longer than ~50 lines — usually doing too much, split.
- Ports that return framework types (SQL rows, HTTP responses) instead of domain types.
