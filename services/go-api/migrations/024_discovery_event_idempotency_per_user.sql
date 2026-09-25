-- Scope the telemetry idempotency key to its owner (#2245).
--
-- event_id is client-supplied and validated only as a non-nil UUID, but 006
-- keyed it globally, so `ON CONFLICT (event_id) DO NOTHING` dropped any event
-- carrying an id another account had already used: a client replaying a
-- borrowed or guessed id silenced the victim's library_add / wrong_album — the
-- events that feed ranking — and Append still reported success. Keyed on
-- (user_id, event_id), a replayed id can only collide with its own author.
--
-- Safe on existing data: the index below replaces a strictly stronger one, so
-- no two rows can already share the pair. user_id is NOT NULL, so the pair
-- never decays into the NULLs-are-distinct case that dedups nothing.
--
-- Create-then-drop inside the runner's default single transaction: the swap is
-- atomic to every other session, so no window exists where the table carries
-- neither key and a retry could double-insert. That atomicity is why this build
-- is plain rather than CONCURRENTLY; it holds a write lock on discovery_events
-- for the build, which retention keeps short.
--
-- Deploy order: prod-migrate.sh runs before the blue-green swap, so between
-- this file and the new binary the old one's `ON CONFLICT (event_id)` matches
-- no index and every append fails with 42P10. The label-critical tier is an
-- at-least-once client outbox and re-sends; the fire-and-forget tier loses that
-- window. Rollback is the reverse pair, and only alongside the old binary.
CREATE UNIQUE INDEX IF NOT EXISTS uq_discovery_events_user_event_id
    ON discovery_events (user_id, event_id)
    WHERE event_id IS NOT NULL;

DROP INDEX IF EXISTS uq_discovery_events_event_id;
