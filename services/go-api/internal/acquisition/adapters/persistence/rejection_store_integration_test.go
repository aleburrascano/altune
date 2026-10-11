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

	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
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

	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
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

	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
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

	keys, err := store.ActiveKeys(ctx, trackA, time.Now().Add(-30*24*time.Hour))
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

	keys, err := store.ActiveKeys(context.Background(), uuid.NewString(), time.Now().Add(-30*24*time.Hour))
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

	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
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

	_, err := store.ActiveKeys(ctx, uuid.NewString(), time.Now().Add(-30*24*time.Hour))
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
	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 0 {
		t.Errorf("ActiveKeys(...) = %v, want none after a failed batch", keys)
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
	keys, err := store.ActiveKeys(ctx, trackID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("ActiveKeys(...) = %v, want nil", err)
	}
	if len(keys) != 2 {
		t.Errorf("ActiveKeys(...) = %v, want two keys", keys)
	}
}
