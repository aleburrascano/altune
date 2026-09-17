-- Bound library list/search cost by page size instead of library size (#1050).
--
-- Before this, nothing on tracks supported "WHERE user_id = $1 ORDER BY <sort>",
-- so every page scanned and sorted the user's whole track set before LIMIT, and
-- the ILIKE search filters had no index at all (a seq scan across every user).
--
-- migrate:no-transaction
--
-- APPLY WITH PLAIN `psql -f` (autocommit). CREATE INDEX CONCURRENTLY cannot run
-- inside a transaction block, so do NOT use `psql -1` / --single-transaction or
-- wrap this file in BEGIN/COMMIT. The marker above is what tells deploy/lib.sh's
-- runner to drop --single-transaction for this file. CONCURRENTLY builds without
-- blocking writes to tracks. The code does not depend on these indexes: before
-- this is applied the same queries return the same rows, just via scans.
--
-- If a build fails midway, Postgres leaves an INVALID index that IF NOT EXISTS
-- will then skip. Check with
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
-- and DROP INDEX CONCURRENTLY the invalid one before re-running this file.

-- One index per supported track sort (library_lens_repo.go trackOrderBy). The
-- key order and directions match each ORDER BY exactly, so the planner reads a
-- page straight off the index and stops after OFFSET+LIMIT rows. The default
-- (added_at) index is also the narrow index the separate count(*) uses.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_added_at
    ON tracks (user_id, added_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_lower_title
    ON tracks (user_id, lower(title), id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_user_year
    ON tracks (user_id, year DESC NULLS LAST, id DESC);

-- Trigram GIN indexes back the substring ILIKE '%term%' filters used by the
-- track, album, and artist lenses. One per searched column so each OR branch
-- becomes a bitmap index scan. Terms shorter than three characters yield no
-- trigrams and still fall back to the user_id-scoped scan.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_title_trgm
    ON tracks USING gin (title gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_artist_trgm
    ON tracks USING gin (artist gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_album_trgm
    ON tracks USING gin (album gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tracks_album_artist_trgm
    ON tracks USING gin (album_artist gin_trgm_ops);
