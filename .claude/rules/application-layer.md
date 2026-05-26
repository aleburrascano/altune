---
paths:
  - "services/api/**/application/**"
---

# Application layer — use cases and ports

The application layer is **the orchestrator**. It defines use cases, holds the port interfaces the domain needs, and coordinates between domain operations + I/O.

## Imports allowed

- `domain/` (freely)
- Python standard library
- Type-only imports of pydantic for DTOs at the application boundary (optional — most prefer plain dataclasses)

## Imports FORBIDDEN

- `adapters/` (concrete implementations) — application defines ports; adapters implement them
- FastAPI, SQLAlchemy, httpx, Redis, Kafka — anything framework-specific

## What lives here

- **Use cases / application services** — `register_track.py`, `enqueue_play.py`. One file per use case (or tight cluster of related use cases).
- **Ports** — abstract interfaces (`Protocol` or ABC) that the use case calls. `TrackRepository`, `MetadataProvider`, `PlayQueue`, etc.
- **DTOs** — input/output shapes for use cases. Plain dataclasses; pydantic only if validation needed at this layer.
- **Transactions** — the use case is the unit-of-work boundary. Begin/commit/rollback orchestration lives here, behind a port.

## Port discipline

- Port interfaces are owned by the application layer. Adapters in `adapters/outbound/` implement them.
- Port methods speak in **domain types**, not framework types. `find_by_id(track_id: TrackId) -> Track | None`, not `find_by_id(id: str) -> dict`.
- Keep ports small (Interface Segregation Principle — `[vault: wiki/concepts/Interface Segregation Principle.md]`). A port with 12 methods is usually 3 ports in a trench coat.

## Use case shape

```python
@dataclass(frozen=True)
class RegisterTrackInput:
    title: str
    artist: str
    duration_ms: int

@dataclass(frozen=True)
class RegisterTrackOutput:
    track_id: TrackId

class RegisterTrack:
    def __init__(self, tracks: TrackRepository, events: DomainEventPublisher) -> None:
        self._tracks = tracks
        self._events = events

    async def execute(self, input: RegisterTrackInput) -> RegisterTrackOutput:
        track = Track.create(title=input.title, artist=input.artist, duration_ms=input.duration_ms)
        await self._tracks.save(track)
        for event in track.pull_events():
            await self._events.publish(event)
        return RegisterTrackOutput(track_id=track.id)
```

## DI / wiring

- Use cases receive ports through `__init__`. **No global state.**
- Wiring (which concrete adapter implements which port) happens in `platform/container.py` — see [vault: wiki/concepts/Dependency Injection.md].

## Testing

- Use cases unit-tested with **in-memory adapter implementations** (`InMemoryTrackRepository`, `RecordingEventPublisher`).
- No database, no HTTP. Tests are fast (<100ms each).
- Test the **behavior** (input → output + side effects on ports), not the implementation.

## Anti-patterns

- Use cases that import from `adapters/` — port discipline broken.
- Use cases that contain domain logic — push it into the aggregate.
- Use cases longer than ~50 lines — usually doing too much, split.
- Anemic ports (just CRUD wrappers around the repo) — model the *intent*, not the SQL.
