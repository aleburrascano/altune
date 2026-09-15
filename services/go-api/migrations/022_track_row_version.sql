-- Monotonic row-version column for optimistic-lock CAS on track writes (#1419,
-- follow-up to #966). #966 fixed the headline lost-update with a column-scoped
-- acquisition Update that no longer clobbers user metadata. Same-column races —
-- two acquisition settles racing, or the stale-pending sweeper vs a settle —
-- still resolved last-writer-wins with no conflict surfaced.
--
-- This column lets PgxTrackRepository.Update run as a compare-and-swap: it
-- matches an owned row at an expected version, bumps the version on write, and
-- surfaces a distinct conflict error when the version has already advanced,
-- instead of silently overwriting the other writer's result.
--
-- Starts at 0 for every existing and new row (DEFAULT), so the first write
-- reads version 0, expects 0, and lands version 1. Existing writers that do not
-- yet pass an expected version cannot compile against the new signature, so no
-- write bypasses the predicate.
ALTER TABLE tracks
    ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 0;
