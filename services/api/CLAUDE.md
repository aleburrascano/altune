# Altune API (Python + FastAPI + hexagonal) — local rules

Universal coding discipline → `~/.claude/CLAUDE.md`. Project constitution → `<repo>/CLAUDE.md`. Python-wide rules → `.claude/rules/python-backend.md`. Layer rules → `.claude/rules/{domain,application,adapters}-layer.md`. **This file: backend platform quirks only.**

## Stack

- Python 3.12+ (`pyproject.toml` `requires-python = ">=3.12"`).
- FastAPI + Pydantic v2 + Uvicorn.
- structlog for logging.
- `uv` for env/deps (`uv sync`, `uv run`).
- `ruff` for lint+format, `mypy --strict` for types, `pytest` (+ `pytest-asyncio`, `pytest-cov`, `hypothesis`, `respx`, `testcontainers`).

## Hexagonal layout

```
src/altune/
├── domain/        # pure business model, no framework imports
├── application/   # use cases + port interfaces
├── adapters/
│   ├── inbound/   # HTTP routers, CLI, message consumers
│   └── outbound/  # DB repositories, external HTTP, message publishers
└── platform/      # config, DI container, logging, observability
```

See `docs/architecture.md` for the full rule set. The `.claude/rules/{domain,application,adapters}-layer.md` files enforce per-layer constraints.

## Running

```bash
# Install / sync
uv sync --all-extras

# Run server (dev)
uv run uvicorn altune.platform.app:app --reload --host 0.0.0.0 --port 8000

# Run tests
uv run pytest                       # all
uv run pytest -m unit               # unit only
uv run pytest -m integration        # integration only

# Lint + format + type
uv run ruff check src tests
uv run ruff format src tests
uv run mypy src tests
```

## Adding a dependency

1. `/brainstorm-tech-choice` first (vault lookup + ADR if non-trivial).
2. `uv add <pkg>` for runtime, `uv add --dev <pkg>` for dev.
3. Commit lockfile in same commit as the `pyproject.toml` change.

## Testing setup

- `tests/unit/` — domain + application only. **No DB, no network.** Use in-memory adapter implementations.
- `tests/integration/` — adapters against real-ish dependencies via `testcontainers`.
- `tests/e2e/` — full stack via `httpx.AsyncClient` against `app = create_app()` (no real network for the app itself; testcontainers for DB).

Test naming: `test_<unit>_<scenario>` (e.g., `test_track_register_play_increments_count`).

## Async discipline

- All handlers `async def`.
- All I/O via `httpx.AsyncClient` (never `requests`).
- DB drivers: async (`asyncpg`, `aiosqlite`) when persistence lands.
- CPU-bound work in `asyncio.to_thread` or a worker queue.
- No `time.sleep` in async paths — `asyncio.sleep`.

## Configuration

- `pydantic-settings` for typed config in `platform/config.py`.
- Env vars in production; `.env` for local dev (gitignored); `.env.example` checked in.
- `Settings` is constructed once in `platform/app.py` and injected via FastAPI `Depends`.

## Migrations

Not yet — no persistence in scaffold. When added:
- Alembic for SQLAlchemy schemas.
- `.claude/rules/migrations.md` enforces immutability of shipped migrations.

## Anti-patterns

- `print()` for logging — use structlog.
- `dict[str, Any]` in domain code (use `TypedDict`/`Protocol`).
- `requests` library (use `httpx`).
- Sync I/O in async paths.
- Business logic in FastAPI routers.
