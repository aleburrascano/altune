# Domain layer

Purity rules are enforced by `.claude/rules/domain-layer.md`. **Restated here for proximity to the code:**

- No imports from `adapters/`, `platform/`, FastAPI, Pydantic, SQLAlchemy, httpx, or any framework.
- Standard library + other `domain/` modules only.
- Entities have opaque `Id` value objects, not raw `str`/`int`.
- Value objects are `@dataclass(frozen=True)` with attribute equality.
- Aggregates enforce invariants on every state change; events raised internally and pulled via `pull_events()`.
- Domain exceptions in `<context>/exceptions.py`.

`domain-modeler` subagent reviews changes here against DDD tactical patterns + the software-architecture-design vault.

See `docs/ubiquitous-language.md` for terminology. Add new domain terms to the glossary in the same commit you introduce them in code — the `terminology-drift` hook will flag missing entries.
