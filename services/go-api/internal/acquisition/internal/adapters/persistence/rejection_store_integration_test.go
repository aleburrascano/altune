package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func activeKeys(store *PgxRejectionStore, ctx context.Context, trackID string, since time.Time) ([]string, error) {
	prior, err := store.Active(ctx, trackID, since)
	keys := make([]string, len(prior))
	for i, p := range prior {
		keys[i] = p.SourceKey
	}
	return keys, err
}

func TestPgxRejectionStore_ActiveReturnsTheRecordedBasis(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()
	basis := ports.RejectionBasis{ExpectedSeconds: 322.5, ReferenceMBID: "mbid-1"}

	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, 'legacy', 'duration', now())`,
		trackID); err != nil {
		t.Fatalf("seed legacy rejection: %v", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "with-basis", Reason: "duration", Basis: basis},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	prior, err := store.Active(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Active(...) = %v, want nil", err)
	}
	if len(prior) != 2 || prior[0].SourceKey != "legacy" || prior[1].SourceKey != "with-basis" {
		t.Fatalf("Active(...) = %+v, want [legacy with-basis]", prior)
	}
	if prior[0].Basis != nil {
		t.Errorf("legacy basis = %+v, want nil for a NULL basis", *prior[0].Basis)
	}
	if prior[1].Basis == nil || *prior[1].Basis != basis {
		t.Errorf("recorded basis = %v, want %+v", prior[1].Basis, basis)
	}
}

func TestPgxRejectionStore_RecordThenActiveKeysReturnsRecordedKeys(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "no_match", Detail: "bad hash"},
		{TrackID: trackID, SourceKey: "source-b", Reason: "checksum_mismatch"},
	})
	if err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 2 || keys[0] != "source-a" || keys[1] != "source-b" {
		t.Errorf("ActiveKeys(...) = %v, want [source-a source-b]", keys)
	}
}

func TestPgxRejectionStore_RecordUpsertsOnTrackAndSourceKey(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "no_match", Detail: "first"},
	}); err != nil {
		t.Fatalf("first Record(...) = %v, want nil", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "checksum_mismatch", Detail: "second"},
	}); err != nil {
		t.Fatalf("second Record(...) = %v, want nil", err)
	}

	rows, err := pool.Query(ctx, `SELECT reason, detail FROM acquisition_rejections WHERE track_id = $1 AND source_key = $2`, trackID, "source-a")
	if err != nil {
		t.Fatalf("query acquisition_rejections: %v", err)
	}
	defer rows.Close()

	var got int
	var reason, detail string
	for rows.Next() {
		got++
		if err := rows.Scan(&reason, &detail); err != nil {
			t.Fatalf("scan acquisition_rejections row: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate acquisition_rejections rows: %v", err)
	}
	if got != 1 {
		t.Fatalf("got %d rows for (track_id, source_key), want 1 (upsert)", got)
	}
	if reason != "checksum_mismatch" || detail != "second" {
		t.Errorf("row = (reason=%q, detail=%q), want (reason=%q, detail=%q) from the later Record", reason, detail, "checksum_mismatch", "second")
	}
}

func TestPgxRejectionStore_ActiveKeysExcludesKeysRejectedBeforeSince(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, $2, $3, now() - interval '31 days')`,
		trackID, "stale-source", "no_match"); err != nil {
		t.Fatalf("seed stale rejection: %v", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "fresh-source", Reason: "no_match"},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 1 || keys[0] != "fresh-source" {
		t.Errorf("ActiveKeys(...) = %v, want [fresh-source]", keys)
	}
}

func TestPgxRejectionStore_RecordRejectsEmptyTrackID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: "", SourceKey: "source-a", Reason: "no_match"},
	})
	if err == nil {
		t.Fatal("Record with an empty TrackID = nil, want an error from the CHECK constraint")
	}
}

func TestPgxRejectionStore_RecordRejectsEmptySourceKey(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: uuid.NewString(), SourceKey: "", Reason: "no_match"},
	})
	if err == nil {
		t.Fatal("Record with an empty SourceKey = nil, want an error from the CHECK constraint")
	}
}

func TestPgxRejectionStore_RecordWithCancelledContextReturnsError(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: uuid.NewString(), SourceKey: "source-a", Reason: "no_match"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Record with a cancelled context = %v, want an error wrapping context.Canceled", err)
	}
}

func TestPgxRejectionStore_RecordOnClosedPoolReturnsError(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	pool.Close()

	err := store.Record(context.Background(), []ports.CandidateRejectionRecord{
		{TrackID: uuid.NewString(), SourceKey: "source-a", Reason: "no_match"},
	})
	if err == nil {
		t.Error("Record on a closed pool = nil, want an error")
	}
}

func TestPgxRejectionStore_ReRecordingAStaleKeyMakesItActiveAgain(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, $2, $3, now() - interval '31 days')`,
		trackID, "soundcloud.com/bran-van-3000/drinking-in-l-a-3", "no_match"); err != nil {
		t.Fatalf("seed stale rejection: %v", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "soundcloud.com/bran-van-3000/drinking-in-l-a-3", Reason: "drm"},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 1 || keys[0] != "soundcloud.com/bran-van-3000/drinking-in-l-a-3" {
		t.Errorf("ActiveKeys(...) = %v, want [soundcloud.com/bran-van-3000/drinking-in-l-a-3] after re-recording refreshed rejected_at", keys)
	}
}

func TestPgxRejectionStore_ActiveKeysOnlyReturnsTheRequestedTracksKeys(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackA := uuid.NewString()
	trackB := uuid.NewString()

	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackA, SourceKey: "shared-source", Reason: "drm"},
		{TrackID: trackB, SourceKey: "shared-source", Reason: "no_match"},
		{TrackID: trackB, SourceKey: "only-b-source", Reason: "no_match"},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackA, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 1 || keys[0] != "shared-source" {
		t.Errorf("ActiveKeys(trackA) = %v, want [shared-source]", keys)
	}
}

func TestPgxRejectionStore_ActiveKeysForATrackWithNoRejectionsIsEmpty(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)

	keys, err := activeKeys(store, context.Background(), uuid.NewString(), time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 0 {
		t.Errorf("ActiveKeys(...) = %v, want no keys", keys)
	}
}

func TestPgxRejectionStore_SourceKeyWithQuotesAndUnicodeRoundTripsVerbatim(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()
	hostile := `https://example.com/watch?v=x'); DROP TABLE acquisition_rejections;-- "Beyoncé" ☃`

	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: hostile, Reason: "qualifier", Detail: `it's "live"`},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 1 || keys[0] != hostile {
		t.Errorf("ActiveKeys(...) = %q, want [%q]", keys, hostile)
	}
}

func TestPgxRejectionStore_ActiveKeysWithCancelledContextReturnsError(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := activeKeys(store, ctx, uuid.NewString(), time.Now().Add(-30*24*time.Hour))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ActiveKeys with a cancelled context = %v, want an error wrapping context.Canceled", err)
	}
}

func TestPgxRejectionStore_RecordWithNoRecordsSucceeds(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)

	if err := store.Record(context.Background(), nil); err != nil {
		t.Errorf("Record(nil) = %v, want nil", err)
	}
}

func TestPgxRejectionStore_RecordBatchWithOneInvalidRecordStoresNothing(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "valid-source", Reason: "no_match"},
		{TrackID: trackID, SourceKey: "", Reason: "no_match"},
	})
	if err == nil {
		t.Fatal("Record with an empty SourceKey in the batch = nil, want an error")
	}
	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 0 {
		t.Errorf("ActiveKeys(...) = %v, want none after a failed batch", keys)
	}
}

func TestPgxRejectionStore_RecordWithDuplicateKeyInBatchKeepsLastAndStoresOthers(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "no_match", Detail: "first pass"},
		{TrackID: trackID, SourceKey: "source-b", Reason: "drm"},
		{TrackID: trackID, SourceKey: "source-a", Reason: "checksum_mismatch", Detail: "second pass"},
	})
	if err != nil {
		t.Fatalf("Record(...) with a duplicate key in the batch = %v, want nil", err)
	}

	rows, err := pool.Query(ctx, `SELECT source_key, reason, detail FROM acquisition_rejections WHERE track_id = $1 ORDER BY source_key`, trackID)
	if err != nil {
		t.Fatalf("query acquisition_rejections: %v", err)
	}
	defer rows.Close()

	type row struct{ sourceKey, reason, detail string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.sourceKey, &r.reason, &r.detail); err != nil {
			t.Fatalf("scan acquisition_rejections row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate acquisition_rejections rows: %v", err)
	}

	want := []row{
		{sourceKey: "source-a", reason: "checksum_mismatch", detail: "second pass"},
		{sourceKey: "source-b", reason: "drm", detail: ""},
	}
	if len(got) != len(want) {
		t.Fatalf("rows for track = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPgxRejectionStore_ConcurrentRecordsOfTheSameKeysAllSucceed(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			errs <- store.Record(ctx, []ports.CandidateRejectionRecord{
				{TrackID: trackID, SourceKey: "source-a", Reason: "no_match"},
				{TrackID: trackID, SourceKey: "source-b", Reason: "drm"},
			})
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Record(...) = %v, want nil", err)
		}
	}
	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 2 {
		t.Errorf("ActiveKeys(...) = %v, want two keys", keys)
	}
}

func TestPgxRejectionStore_PruneDeletesRowsOlderThanRetentionAndKeepsNewer(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := insertFailedTrack(t, pool).ID.UUID().String()

	for _, seed := range []struct {
		key string
		age string
	}{{"ancient", "61 days"}, {"inside-lookback", "45 days"}, {"fresh", "1 day"}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, $2, 'no_match', now() - $3::interval)`,
			trackID, seed.key, seed.age); err != nil {
			t.Fatalf("seed rejection %s: %v", seed.key, err)
		}
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	keys, err := activeKeys(store, ctx, trackID, time.Now().Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 2 || keys[0] != "fresh" || keys[1] != "inside-lookback" {
		t.Errorf("keys after Prune = %v, want [fresh inside-lookback]", keys)
	}
}

func TestPgxRejectionStore_PruneRemovesMoreRowsThanOneBatch(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()
	total := pruneBatchSize + 5

	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at)
		 SELECT $1, 'k' || g, 'no_match', now() - interval '100 days' FROM generate_series(1, $2) g`,
		trackID, total); err != nil {
		t.Fatalf("seed rejections: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM acquisition_rejections WHERE track_id = $1`, trackID).Scan(&left); err != nil {
		t.Fatalf("count rejections: %v", err)
	}
	if left != 0 {
		t.Errorf("rejections left after Prune = %d, want 0", left)
	}
}

func TestPgxRejectionStore_ARejectionJudgedWithNoLengthOrMBIDKeepsAnEmptyBasisNotNone(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "unknown-length", Reason: "identity", Basis: ports.RejectionBasis{}},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	prior, err := store.Active(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Active(...) = %v, want nil", err)
	}
	if len(prior) != 1 {
		t.Fatalf("Active(...) = %+v, want one rejection", prior)
	}
	if prior[0].Basis == nil || *prior[0].Basis != (ports.RejectionBasis{}) {
		t.Errorf("basis = %v, want a recorded basis of 0s and no MBID, not the nil of a legacy row", prior[0].Basis)
	}
}

func TestPgxRejectionStore_ReRecordingALegacyRowWritesItsBasis(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()
	basis := ports.RejectionBasis{ExpectedSeconds: 201, ReferenceMBID: "mbid-now"}

	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, 'legacy', 'duration', now() - interval '2 days')`,
		trackID); err != nil {
		t.Fatalf("seed legacy rejection: %v", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "legacy", Reason: "duration", Basis: basis},
	}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}

	prior, err := store.Active(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Active(...) = %v, want nil", err)
	}
	if len(prior) != 1 || prior[0].Basis == nil || *prior[0].Basis != basis {
		t.Errorf("Active(...) = %+v, want the legacy row rewritten with basis %+v", prior, basis)
	}
}

func TestPgxRejectionStore_ReRecordingOnANewBasisReplacesTheOldOne(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()
	newer := ports.RejectionBasis{ExpectedSeconds: 0, ReferenceMBID: ""}

	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "duration", Basis: ports.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: "mbid-old"}},
	}); err != nil {
		t.Fatalf("first Record(...) = %v, want nil", err)
	}
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{
		{TrackID: trackID, SourceKey: "source-a", Reason: "fingerprint", Basis: newer},
	}); err != nil {
		t.Fatalf("second Record(...) = %v, want nil", err)
	}

	prior, err := store.Active(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("Active(...) = %v, want nil", err)
	}
	if len(prior) != 1 || prior[0].Basis == nil || *prior[0].Basis != newer {
		t.Errorf("Active(...) = %+v, want one row carrying the later basis %+v", prior, newer)
	}
}

func TestPgxRejectionStore_PruneRemovesRowsOfADeletedTrack(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	kept := insertFailedTrack(t, pool).ID.UUID().String()
	deleted := insertFailedTrack(t, pool).ID.UUID().String()
	for _, trackID := range []string{kept, deleted} {
		if err := store.Record(ctx, []ports.CandidateRejectionRecord{{TrackID: trackID, SourceKey: "source-a", Reason: "no_match"}}); err != nil {
			t.Fatalf("Record(...) = %v, want nil", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, deleted); err != nil {
		t.Fatalf("delete track: %v", err)
	}

	pruned, err := store.Prune(ctx, time.Now())
	if err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	since := time.Now().Add(-30 * 24 * time.Hour)
	if keys, _ := activeKeys(store, ctx, deleted, since); len(keys) != 0 {
		t.Errorf("rejections of the deleted track after Prune = %v, want none", keys)
	}
	if keys, _ := activeKeys(store, ctx, kept, since); len(keys) != 1 {
		t.Errorf("rejections of the live track after Prune = %v, want one", keys)
	}
	if pruned < 1 {
		t.Errorf("Prune(...) = %d, want the orphan counted", pruned)
	}
}

func countRejectionsOf(t *testing.T, store *PgxRejectionStore, trackID string) int {
	t.Helper()
	keys, err := activeKeys(store, context.Background(), trackID, time.Now().Add(-10*365*24*time.Hour))
	if err != nil {
		t.Fatalf("count rejections: %v", err)
	}
	return len(keys)
}

func TestPgxRejectionStore_PruneKeepsARejectionStoredUnderTheUppercaseIDOfALiveTrack(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	var upper string
	if err := pool.QueryRow(ctx,
		`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES (upper($1), 'source-a', 'no_match', now()) RETURNING track_id`,
		live).Scan(&upper); err != nil {
		t.Fatalf("seed rejection: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := countRejectionsOf(t, store, upper); got != 1 {
		t.Errorf("rejections under %q after Prune = %d, want 1 (its track %s is live)", upper, got, live)
	}
}

func TestPgxRejectionStore_PruneSucceedsWhenATrackIDIsNotAUUID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	notUUID := "not-a-uuid-" + uuid.NewString()
	for _, trackID := range []string{notUUID, live} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, 'source-a', 'no_match', now())`,
			trackID); err != nil {
			t.Fatalf("seed rejection: %v", err)
		}
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) with a non-uuid track id stored = %v, want nil", err)
	}

	if got := countRejectionsOf(t, store, live); got != 1 {
		t.Errorf("rejections of the live track after Prune = %d, want 1", got)
	}
}

func TestPgxRejectionStore_PruneKeepsRowsOfATrackDeletedAndRecreatedWithTheSameID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	track := insertFailedTrack(t, pool)
	trackID := track.ID.UUID().String()
	if err := store.Record(ctx, []ports.CandidateRejectionRecord{{TrackID: trackID, SourceKey: "source-a", Reason: "no_match"}}); err != nil {
		t.Fatalf("Record(...) = %v, want nil", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("delete track: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status) VALUES ($1, $2, $3, $4, $5, $6, 'failed')`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString()); err != nil {
		t.Fatalf("re-create track: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := countRejectionsOf(t, store, trackID); got != 1 {
		t.Errorf("rejections of the re-created track after Prune = %d, want 1", got)
	}
}

func TestPgxRejectionStore_PruneRemovesOrphansFillingExactlyTwoBatchesAndKeepsLiveRows(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	deleted := insertFailedTrack(t, pool)
	deletedID := deleted.ID.UUID().String()
	for _, trackID := range []string{live, deletedID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at)
			 SELECT $1, 'k' || g, 'no_match', now() FROM generate_series(1, $2) g`,
			trackID, 2*pruneBatchSize); err != nil {
			t.Fatalf("seed rejections: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, deleted.ID.UUID()); err != nil {
		t.Fatalf("delete track: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := countRejectionsOf(t, store, deletedID); got != 0 {
		t.Errorf("rejections of the deleted track after Prune = %d, want 0", got)
	}
	if got := countRejectionsOf(t, store, live); got != 2*pruneBatchSize {
		t.Errorf("rejections of the live track after Prune = %d, want %d", got, 2*pruneBatchSize)
	}
}

func TestPgxRejectionStore_PruneCountsAnOrphanOlderThanRetentionOnce(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxRejectionStore(pool)
	ctx := context.Background()
	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("draining Prune(...) = %v, want nil", err)
	}
	live := insertFailedTrack(t, pool).ID.UUID().String()
	orphan := uuid.NewString()
	for _, seed := range []struct {
		trackID string
		key     string
		age     string
	}{
		{live, "live-old-1", "61 days"},
		{live, "live-old-2", "100 days"},
		{live, "live-fresh", "1 day"},
		{orphan, "orphan-fresh-1", "1 day"},
		{orphan, "orphan-fresh-2", "2 days"},
		{orphan, "orphan-old", "61 days"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_rejections (track_id, source_key, reason, rejected_at) VALUES ($1, $2, 'no_match', now() - $3::interval)`,
			seed.trackID, seed.key, seed.age); err != nil {
			t.Fatalf("seed rejection %s: %v", seed.key, err)
		}
	}

	pruned, err := store.Prune(ctx, time.Now())
	if err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if pruned != 5 {
		t.Errorf("Prune(...) = %d, want 5 (2 expired live rows + 3 orphan rows, one of them also expired)", pruned)
	}
	if got := countRejectionsOf(t, store, orphan); got != 0 {
		t.Errorf("orphan rejections after Prune = %d, want 0", got)
	}
	if keys, _ := activeKeys(store, ctx, live, time.Now().Add(-365*24*time.Hour)); len(keys) != 1 || keys[0] != "live-fresh" {
		t.Errorf("live track keys after Prune = %v, want [live-fresh]", keys)
	}
}
