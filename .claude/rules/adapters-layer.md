---
paths:
  - "services/api/**/adapters/**"
---

# Adapters layer — drivers and driven

Adapters are the **outer ring**. They translate between the framework world (HTTP, SQL, message brokers, external APIs) and the application layer's ports.

## Two flavors

- **Inbound (driving) adapters** — `adapters/inbound/`. The outside calls these. They turn external requests into use case calls.
  - `inbound/http/` — FastAPI routers
  - `inbound/cli/` — Typer/Click commands (if/when we add a CLI)
  - `inbound/jobs/` — scheduled workers, queue consumers
- **Outbound (driven) adapters** — `adapters/outbound/`. The application calls these (through ports).
  - `outbound/persistence/` — repository implementations (SQLAlchemy/asyncpg)
  - `outbound/external/` — third-party HTTP clients, S3, Spotify API, etc.
  - `outbound/messaging/` — event publishers (Redis pub/sub, Kafka)

See `[vault: wiki/concepts/Hexagonal Architecture.md]`.

## Imports allowed

- `application/` ports + DTOs
- `domain/` types (to construct domain objects from row data)
- Framework code (FastAPI, SQLAlchemy, httpx, etc.)
- `platform/` (config, logging)

## Imports FORBIDDEN

- Other adapters cross-importing — adapters are siblings, they coordinate through the application layer.

## Inbound HTTP adapter shape

```python
@router.post("/tracks", status_code=201, response_model=TrackResponse)
async def register_track(
    body: RegisterTrackBody,
    use_case: RegisterTrack = Depends(deps.register_track_use_case),
) -> TrackResponse:
    output = await use_case.execute(body.to_input())
    return TrackResponse(id=output.track_id.value)
```

The router is a **5-line shell**: parse → call use case → serialize. No business logic.

## Outbound persistence adapter shape

```python
class SqlAlchemyTrackRepository(TrackRepository):
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def save(self, track: Track) -> None:
        row = TrackRow.from_domain(track)
        await self._session.merge(row)

    async def find_by_id(self, id: TrackId) -> Track | None:
        row = await self._session.get(TrackRow, id.value)
        return row.to_domain() if row else None
```

The repository owns the SQL ↔ domain mapping. Domain objects never see SQLAlchemy models.

## Testing

- Inbound adapters tested with FastAPI `TestClient` against in-memory use cases (use case is the seam).
- Outbound persistence adapters tested with `testcontainers` (real Postgres). These are **integration tests**, in `tests/integration/`.
- Outbound external adapters tested with HTTP mocking (`respx` for httpx).

## Anti-patterns

- Business logic in routers (validation that the domain should own, conditionals on domain state).
- Repositories returning framework types (rows, ORM models) instead of domain types.
- Bypassing the use case ("just call the repo from the router for this quick fix"). **Always go through the use case** — the use case is where transactions begin/end.
- Adapter constructing domain objects with invalid state. The domain enforces invariants; the adapter is just a translator.
