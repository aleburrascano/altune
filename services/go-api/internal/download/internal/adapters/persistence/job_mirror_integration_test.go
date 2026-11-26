package persistence_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/dbtest"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const jobRowMatchesTrackSQL = `
SELECT EXISTS (
	SELECT 1 FROM tracks t JOIN acquisition_jobs j ON j.track_id = t.id
	WHERE t.id = $1
		AND j.job_kind = t.acquisition_job_kind
		AND j.available_at IS NOT DISTINCT FROM t.acquisition_available_at
		AND j.lease_until IS NOT DISTINCT FROM t.acquisition_lease_until
		AND j.attempts = t.acquisition_attempts
		AND j.run_attempts = t.acquisition_run_attempts
		AND j.correlation_id IS NOT DISTINCT FROM t.acquisition_correlation_id
		AND j.started_at IS NOT DISTINCT FROM t.acquisition_started_at)`

func requireJobRowMirrorsTrack(t *testing.T, pool *pgxpool.Pool, id sharedids.TrackId, after string) {
	t.Helper()
	var matches bool
	if err := pool.QueryRow(context.Background(), jobRowMatchesTrackSQL, id.UUID()).Scan(&matches); err != nil {
		t.Fatalf("compare the job row after %s: %v", after, err)
	}
	if !matches {
		t.Fatalf("acquisition_jobs row differs from the track's job columns after %s", after)
	}
}

func TestPostgresJobQueue_EveryQueueStepLeavesTheJobRowEqualToTheTrackColumns(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertFailedTrack(t, pool)
	requireJobRowMirrorsTrack(t, pool, track.ID, "the insert")

	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "enqueue")

	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "claim")

	if err := queue.Heartbeat(ctx, track.ID, job.ClaimNumber, time.Minute); err != nil {
		t.Fatalf("Heartbeat = %v", err)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "heartbeat")

	if err := queue.Release(ctx, track.ID, job.ClaimNumber, time.Second); err != nil {
		t.Fatalf("Release = %v", err)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "release")

	if err := queue.Settle(ctx, track.ID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v", err)
	}
	requireJobRowMirrorsTrack(t, pool, track.ID, "settle")
}

func TestPostgresJobQueue_ADeletedTrackTakesItsJobRowWithIt(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	ctx := context.Background()

	track := insertFailedTrack(t, pool)
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("delete the track: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM acquisition_jobs WHERE track_id = $1`, track.ID.UUID()).Scan(&rows); err != nil {
		t.Fatalf("count job rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("job rows for a deleted track = %d, want 0", rows)
	}
}

func upgradeStateOf(t *testing.T, pool *pgxpool.Pool, id sharedids.TrackId) (map[string]any, []string) {
	t.Helper()
	var raw []byte
	var keys []string
	if err := pool.QueryRow(context.Background(),
		`SELECT evidence, rejected_source_keys FROM acquisition_upgrade_state WHERE track_id = $1`, id.UUID()).Scan(&raw, &keys); err != nil {
		t.Fatalf("read the upgrade state: %v", err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatalf("decode the upgrade state: %v", err)
	}
	return evidence, keys
}

func TestPostgresUpgradeStore_TheStateOwnsTheBookkeepingKeysAndOnlyCopiesThemBackToTracks(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	store := persistence.NewPostgresUpgradeStore(pool)
	ctx := context.Background()

	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","source_kbps":96}`, sourceURL: sourceURL("mirror")})

	if _, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "yt:bad", CountsAsTry: true}); err != nil {
		t.Fatalf("NoteAttempt = %v", err)
	}
	if err := store.GiveUp(ctx, id, 4, "nothing better"); err != nil {
		t.Fatalf("GiveUp = %v", err)
	}
	if _, err := store.NoteInPlaceFailure(ctx, id); err != nil {
		t.Fatalf("NoteInPlaceFailure = %v", err)
	}

	evidence, keys := upgradeStateOf(t, pool, id)
	if !slices.Equal(keys, []string{"yt:bad"}) {
		t.Errorf("rejected_source_keys = %v, want [yt:bad]", keys)
	}
	for _, key := range []string{"upgrade_tried_at", "best_available_generation", "best_available_reason", "best_available_at", "in_place_failures"} {
		if _, ok := evidence[key]; !ok {
			t.Errorf("upgrade state lacks %q, has %v", key, evidence)
		}
	}
	for _, key := range []string{"grade", "source_kbps"} {
		if _, ok := evidence[key]; ok {
			t.Errorf("upgrade state holds the library-owned key %q", key)
		}
	}

	var copiedReason string
	var copiedKeys []string
	if err := pool.QueryRow(ctx, `SELECT acquisition_evidence->>'best_available_reason', rejected_source_keys FROM tracks WHERE id = $1`, id.UUID()).Scan(&copiedReason, &copiedKeys); err != nil {
		t.Fatalf("read the track copy: %v", err)
	}
	if copiedReason != "nothing better" || !slices.Equal(copiedKeys, []string{"yt:bad"}) {
		t.Errorf("track copy = %q, %v, want the state copied back for a rollback", copiedReason, copiedKeys)
	}

	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_evidence = '{"grade":"B"}'::jsonb, rejected_source_keys = '{}' WHERE id = $1`, id.UUID()); err != nil {
		t.Fatalf("write the track evidence: %v", err)
	}
	evidenceAfter, keysAfter := upgradeStateOf(t, pool, id)
	if !slices.Equal(keysAfter, []string{"yt:bad"}) || evidenceAfter["best_available_reason"] != "nothing better" {
		t.Errorf("state after a write to tracks = %v, %v, want it untouched (nothing flows from tracks to the state)", evidenceAfter, keysAfter)
	}
}
