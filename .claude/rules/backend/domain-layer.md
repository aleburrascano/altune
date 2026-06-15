---
paths:
  - "services/go-api/**/domain/**"
---

# Domain layer — purity rules

The domain layer is the **inner hexagon**. It models the business and nothing else.

## Imports allowed

- Go standard library
- Other modules within `domain/`
- Shared value objects from `internal/shared/` (e.g., `shared.UserId`)

## Imports FORBIDDEN

- Anything from `adapters/` (HTTP handlers, persistence, external APIs)
- Anything from `service/` (use cases orchestrate domain, not the reverse)
- Any framework: `chi`, `gin`, `echo`, `gorm`, `sqlx`, `redis`, `kafka`
- Any I/O library: `net/http`, `database/sql`, `os` (except `os.ErrNotExist` for error matching)

## Building blocks (DDD tactical)

Consult `[vault: wiki/concepts/Domain-Driven Design.md]` before defining new domain types.

- **Entity** — has identity that persists. Identity is an opaque `Id` value object (wrapping `uuid.UUID`), not raw `string`. Equality by id.
- **Value Object** — immutable, defined by attributes. Use unexported fields + exported getters. Equality by attribute values.
- **Aggregate** — cluster of entities/VOs with a single root. External code references only the root. The root enforces invariants on every state change.
- **Domain Event** — past-tense, immutable record of something that happened. Struct with `OccurredAt time.Time`. Raised by aggregate methods; consumed by application services.
- **Domain Service** — stateless operation that doesn't belong to any entity/aggregate. Use sparingly — most logic should live on aggregates.

## Examples from this codebase

**Value Object (identity):**
```go
type TrackId struct {
    value uuid.UUID
}
func NewTrackId() TrackId { return TrackId{value: uuid.New()} }
func (t TrackId) UUID() uuid.UUID { return t.value }
func (t TrackId) String() string  { return t.value.String() }
func (t TrackId) IsZero() bool    { return t.value == uuid.Nil }
```

**Enum with zero-value sentinel:**
```go
type AcquisitionStatus int
const (
    AcquisitionPending AcquisitionStatus = iota // 0 = default/pending
    AcquisitionReady
    AcquisitionFailed
)
```

## Invariants

- An aggregate MUST always be in a valid state at method boundaries. Use constructor functions (`NewTrack(...)`) that validate; never allow direct struct construction with invalid fields.
- Raise domain-specific errors on rule breaches. Domain errors live in `domain/<context>/` alongside the types they protect.

## Ubiquitous language

- Names match `docs/ubiquitous-language.md`. If you introduce a new domain term, add it to the glossary in the same commit.

## Anti-patterns

- Anemic domain models (data containers with no behavior) — push logic into the entity/aggregate.
- "Manager" / "Helper" types in `domain/` — usually a missing aggregate or domain service.
- Cross-aggregate transactions — coordinate via domain events, not by reaching into another aggregate.
- Primitive obsession — wrap `string`/`int`/`float64` in value objects when they represent domain concepts (`TrackId`, `PlaylistId`, `UserId`).
