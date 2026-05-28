# discovery persistence adapters — bounded-context local rules

SQLAlchemy mappings + repositories for `discovery_search_history` and `discovery_search_clicks`. Schemas defined by Alembic migrations [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\migrations\versions\d15c001f8eaa_add_discovery_search_history.py] and [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\migrations\versions\e2bcd72a93f1_add_discovery_search_clicks.py].

## Key terms

- **`*Row` class** — SQLAlchemy ORM model. Owns `to_domain` / `from_domain` translation. Domain types never see Row classes — that's the seam.
- **Ring-buffer trim** — `trim_to_n` keeps the latest N rows per user; older rows are deleted by a single `DELETE ... NOT IN (SELECT ... LIMIT N)` statement [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\persistence\discovery\search_history_repository.py#L34-L52].
- **Sliding-window dedup** — `insert_if_outside_window` for clicks. Queries `clicked_at > now() - interval '60s'` for matching `(user_id, query_norm, result_signature)`; if a row exists, return `ClickInsertOutcome(inserted=False, deduped_against_id=<id>)` instead of inserting [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\services\api\src\altune\adapters\outbound\persistence\discovery\search_click_repository.py#L32-L52].

## Patterns specific here

- **`list_distinct_recent` is two-step**: subquery groups by `query_norm` taking MAX(`executed_at`) and limits to N; outer query joins back to fetch full rows. Returns distinct queries newest-first per AC#13.
- **All async via `AsyncSession`.** Repositories take a session in `__init__`; the session lifecycle is owned by the router (`async with sessionmaker() as session: ... await session.commit()`).
- **No `# AIDEV-NOTE` for SQLAlchemy 2.0 `Mapped[T]` runtime-resolution gotcha** — but the catalog precedent has one. Future change: if you see `NameError: UUID` at decoration time, the issue is `from __future__ import annotations` hiding the type. Move `UUID` and `datetime` imports OUT of TYPE_CHECKING (with `# noqa: TC003`).
- **No UNIQUE constraint on the dedup tuple.** Per ADR-0007: idempotency lives application-side (in the repository's sliding-window query) to avoid uniqueness false-positives across the window boundary [VERIFIED:Read@c:\Users\Alessandro\Desktop\altune\docs\adr\0007-unified-music-search.md#L7-L7].
- **`session.flush()` not `commit()`** inside the repo. The router commits the session after the use case completes; the repo's job is only to stage.

## Known gotchas

- **`now() - interval '60s'` is application-side, not DB-side.** Python's `datetime.now(UTC) - timedelta(seconds=60)` builds the threshold; DB timezone behavior doesn't matter as long as both sides are timezone-aware. Postgres `TIMESTAMP(timezone=True)` columns enforce that.
- **`from_domain` / `to_domain` are the only crossing points.** Anywhere else translating between Row and domain is a smell.
- **Index name from the migration is load-bearing**: `discovery_search_history_user_idx` is the index Postgres uses for the `(user_id, executed_at DESC, id DESC)` order. Don't rename without re-running the migration test.
- **Testcontainers integration tests need Docker running.** When Docker is offline, the tests error at fixture setup. They're marked `@pytest.mark.integration`.
