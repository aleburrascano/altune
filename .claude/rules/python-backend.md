---
paths:
  - "services/api/**/*.py"
---

# Python backend rules

## Type strictness

- `mypy --strict` (configured in `pyproject.toml`) — passes on every commit.
- All function signatures fully typed. Use `from __future__ import annotations` at top of every module.
- Prefer `TypedDict`/`Protocol` over `dict[str, Any]`. `Any` is a smell — justify with an inline `# AIDEV-NOTE:` if used.

## Style and lint

- `ruff check --fix` + `ruff format` (configured in `pyproject.toml`).
- Line length 100.
- Imports sorted by ruff (`I` rule).

## Async

- FastAPI handlers are `async def` by default.
- Use `asyncio.TaskGroup` for structured concurrency.
- Never block the event loop — wrap CPU-bound work in `asyncio.to_thread` or push to a worker.

## Pydantic v2

- Request/response models inherit from `pydantic.BaseModel`.
- Domain types are **plain Python** (dataclasses or attrs) — Pydantic is for boundaries only.
- Use `ConfigDict(frozen=True)` for response models.

## FastAPI conventions

- Routers live in `services/api/src/altune/adapters/inbound/http/<bounded-context>/`.
- One router per bounded context. Routers are **thin** — call into application services, never embed domain logic.
- Dependency injection via FastAPI's `Depends()` for repositories, services, current user, etc.
- Error responses are mapped from domain exceptions in a single `exception_handlers.py`.

## Logging & observability

- Structured logging via `structlog` (configured in `platform/logging.py`).
- Every request gets a correlation ID; logs from that request carry it.
- Add `# AIDEV-NOTE:` when a log line is load-bearing for incident response.

## Testing

- pytest. Test paths mirror source: `tests/unit/<package>/test_<module>.py`.
- Unit tests use in-memory adapter implementations — **no DB, no network**.
- Integration tests use `testcontainers` for real Postgres/Redis where applicable.
- E2E tests run against a started server (`pytest-asyncio` + `httpx.AsyncClient`).
- Coverage target: 90%+ on `domain/` and `application/`; lower OK on `adapters/`.

## Do not

- Import from `adapters/` inside `domain/` or `application/`. **The `domain-layer.md` and `application-layer.md` rules will enforce this with examples.**
- Use synchronous I/O in async paths.
- Catch bare `Exception` without re-raising or logging with context.
- Add a dependency without `/brainstorm-tech-choice` + ADR.
