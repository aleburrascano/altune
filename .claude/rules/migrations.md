---
paths:
  - "services/api/**/migrations/**"
  - "services/api/**/alembic/**"
---

# Migrations — shipped migrations are immutable

<important if="modifying a migration that has been merged to main">
**STOP.** Migrations applied to any environment are immutable. Modifying them retroactively desyncs environments and produces ghost state.

To change a shipped migration: add a **new migration** that performs the correction (drop, alter, backfill). Do not edit the original.

The only exception: a migration created in this same uncommitted branch that has never run anywhere. Even then, prefer to add a new one for traceability.
</important>

## Tooling

- Alembic for SQLAlchemy schema migrations (added when persistence enters the project).
- One migration file per logical change. Generated names include the change intent: `add_track_play_count_column.py`, not `update_schema_5.py`.

## Reversibility

- Every migration must implement `downgrade()`. If a change is genuinely irreversible (data loss), explain in the docstring and add `# AIDEV-WARNING: irreversible — <why>`.

## Pre-deploy verification

Before applying a migration to production:
1. **Backup**. The runbook in `docs/workflows/refactor.md` (migration sub-section) covers this.
2. Test on a copy of production data (testcontainers + snapshot).
3. Confirm the `downgrade()` runs successfully on that copy.

## Anti-patterns

- Renaming a column in one migration without backfill — write-then-cutover-then-delete is safer.
- `DROP TABLE` without an explicit ADR.
- Data migrations mixed into schema migrations — split into separate files (schema first, data second).
- Background-incompatible changes deployed atomically with code changes — use feature flags + multi-step deploy.
