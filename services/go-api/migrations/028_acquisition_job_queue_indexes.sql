-- Fairness index for the acquisition job queue (027), built CONCURRENTLY per
-- 020's precedent so the build never takes a table-level lock on tracks.
--
-- migrate:no-transaction
--
-- APPLY WITH PLAIN `psql -f` (autocommit). CREATE INDEX CONCURRENTLY cannot
-- run inside a transaction block, so do NOT use `psql -1` / --single-transaction
-- or wrap this file in BEGIN/COMMIT. The marker above is what tells
-- deploy/lib.sh's runner to drop --single-transaction for this file. The code
-- does not depend on the index: before this is applied, Claim's queries
-- return the same rows, just via scans.
--
-- If a build fails midway, Postgres leaves an INVALID index that IF NOT
-- EXISTS will then skip. Check with
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
-- and DROP INDEX CONCURRENTLY the invalid one before re-running this file.

-- Backs the per-user in-flight fairness subquery in Claim's ORDER BY: without
-- it, each candidate row re-scans a user_id-keyed slice of tracks per Claim
-- call to count that user's other leased-and-pending rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_acquisition_lease_until
    ON tracks (user_id, acquisition_lease_until)
    WHERE acquisition_status = 'pending';
