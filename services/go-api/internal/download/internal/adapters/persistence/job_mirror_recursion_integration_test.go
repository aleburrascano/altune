package persistence_test

import (
	"altune/go-api/internal/shared/dbtest"
	"context"
	"testing"
)

const countingTriggersSQL = `
CREATE TABLE mirror_firings (side TEXT NOT NULL, track_id UUID NOT NULL);
CREATE FUNCTION count_track_firing() RETURNS trigger AS $$
BEGIN INSERT INTO mirror_firings VALUES ('tracks', NEW.id); RETURN NULL; END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION count_job_firing() RETURNS trigger AS $$
BEGIN INSERT INTO mirror_firings VALUES ('jobs', NEW.track_id); RETURN NULL; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER zz_count_track_firing AFTER UPDATE ON tracks FOR EACH ROW EXECUTE FUNCTION count_track_firing();
CREATE TRIGGER zz_count_job_firing AFTER UPDATE ON acquisition_jobs FOR EACH ROW EXECUTE FUNCTION count_job_firing()`

const dropCountingTriggersSQL = `
DROP TRIGGER IF EXISTS zz_count_track_firing ON tracks;
DROP TRIGGER IF EXISTS zz_count_job_firing ON acquisition_jobs;
DROP FUNCTION IF EXISTS count_track_firing();
DROP FUNCTION IF EXISTS count_job_firing();
DROP TABLE IF EXISTS mirror_firings`

func TestJobMirrorTriggers_AWriteOnEitherSideFiresEachSideOnceAndLeavesBothEqual(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, dropCountingTriggersSQL); err != nil {
		t.Fatalf("reset counting triggers: %v", err)
	}
	if _, err := pool.Exec(ctx, countingTriggersSQL); err != nil {
		t.Fatalf("install counting triggers: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), dropCountingTriggersSQL) })

	track := insertFailedTrack(t, pool)

	firings := func(side string) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM mirror_firings WHERE side = $1 AND track_id = $2`, side, track.ID.UUID()).Scan(&n); err != nil {
			t.Fatalf("count firings: %v", err)
		}
		return n
	}
	reset := func() {
		if _, err := pool.Exec(ctx, `DELETE FROM mirror_firings`); err != nil {
			t.Fatalf("reset firings: %v", err)
		}
	}

	reset()
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_attempts = acquisition_attempts + 1, acquisition_job_kind = 'replace' WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("write on the tracks side: %v", err)
	}
	if got := firings("tracks"); got != 1 {
		t.Errorf("tracks row updated %d times after a tracks-side write, want 1", got)
	}
	if got := firings("jobs"); got != 1 {
		t.Errorf("job row updated %d times after a tracks-side write, want 1", got)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "a tracks-side write")

	reset()
	if _, err := pool.Exec(ctx, `UPDATE acquisition_jobs SET attempts = attempts + 1, job_kind = 'acquire' WHERE track_id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("write on the jobs side: %v", err)
	}
	if got := firings("jobs"); got != 1 {
		t.Errorf("job row updated %d times after a jobs-side write, want 1", got)
	}
	if got := firings("tracks"); got != 1 {
		t.Errorf("tracks row updated %d times after a jobs-side write, want 1", got)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "a jobs-side write")
}
