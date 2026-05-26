---
paths:
  - "services/api/**/domain/**"
---

# Domain layer — purity rules

The domain layer is the **inner hexagon**. It models the business and nothing else.

## Imports allowed

- Python standard library
- Other modules within `domain/`
- Type-only imports from `application/` ports (when defining domain interfaces consumed by app services) — but **prefer to keep ports in `application/`**, not here

## Imports FORBIDDEN

- Anything from `adapters/` (HTTP, persistence, external APIs)
- Anything from `platform/` (config, logging frameworks — use plain logging if needed)
- FastAPI, Pydantic, SQLAlchemy, httpx, Redis, Kafka, any framework
- Any third-party library that isn't a pure-Python data utility (e.g., `attrs` is OK, `requests` is not)

The `architecture-reviewer` subagent + the `domain-modeler` subagent enforce this on every change to `domain/`.

## Building blocks (DDD tactical)

Consult `[vault: wiki/concepts/Domain-Driven Design.md]` and `[vault: wiki/topics/DDD Building Blocks.md]` before defining new domain types.

- **Entity** — has identity that persists. Identity defined by an opaque `Id` value object, not raw `str`/`int`. Equality by id.
- **Value Object** — immutable, defined by attributes. Use `@dataclass(frozen=True)` or `attrs.frozen`. Equality by attribute.
- **Aggregate** — cluster of entities/VOs with a single root. External code references only the root. The root enforces invariants on every state change.
- **Domain Event** — past-tense, immutable record of something that happened. Frozen dataclass with a `Self.occurred_at` field. Raised by aggregate methods; consumed by application services.
- **Domain Service** — stateless operation that doesn't belong to any single entity/aggregate. Use sparingly — most logic should be on aggregates.

## Invariants

- An aggregate **must always** be in a valid state at method boundaries. Use private constructors / factory methods if needed to prevent invalid construction.
- Raise domain exceptions (`InvariantViolation`, `<EntityName>NotFound`) on rule breaches. Domain exceptions live in `domain/<context>/exceptions.py`.

## Ubiquitous language

- Names match `docs/ubiquitous-language.md`. If you introduce a new term in the domain, add it to the glossary in the same commit (the `terminology-drift` hook will catch missing entries).

## Anti-patterns (caught by `domain-modeler` subagent)

- Anemic domain models (data containers with no behavior) — push logic into the entity/aggregate.
- "Manager" / "Helper" classes in `domain/` — usually a sign of a missing aggregate or domain service named for what it *does*.
- Cross-aggregate transactions — coordinate via domain events + saga, not by reaching into another aggregate.
- Primitive obsession — wrap `str`/`int`/`float` in value objects when they represent domain concepts (`Email`, `Money`, `TrackDurationMs`).
