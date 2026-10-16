package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type outcomeRow struct {
	outcome     string
	reason      string
	elapsedMs   int64
	completedAt time.Time
}

func queryOutcomeRows(t *testing.T, pool *pgxpool.Pool, trackID string) []outcomeRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT outcome, reason, elapsed_ms, completed_at FROM acquisition_outcomes WHERE track_id = $1 ORDER BY id`,
		trackID)
	if err != nil {
		t.Fatalf("query acquisition_outcomes: %v", err)
	}
	defer rows.Close()

	var got []outcomeRow
	for rows.Next() {
		var r outcomeRow
		if err := rows.Scan(&r.outcome, &r.reason, &r.elapsedMs, &r.completedAt); err != nil {
			t.Fatalf("scan acquisition_outcomes row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate acquisition_outcomes rows: %v", err)
	}
	return got
}

func TestPgxOutcomeStore_RecordsFieldsAndServerTimestamp(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()

	cases := []ports.AcquisitionOutcome{
		{TrackID: uuid.NewString(), Outcome: "succeeded", Reason: "", ElapsedMs: 1200},
		{TrackID: uuid.NewString(), Outcome: "failed", Reason: "no_match_found", ElapsedMs: 340},
		{TrackID: uuid.NewString(), Outcome: "cancelled", Reason: "user-cancelled", ElapsedMs: 15},
	}

	before := time.Now().Add(-time.Second)
	for _, c := range cases {
		if err := store.Record(ctx, c); err != nil {
			t.Fatalf("Record(%+v) = %v, want nil", c, err)
		}
	}
	after := time.Now().Add(time.Second)

	for _, c := range cases {
		rows := queryOutcomeRows(t, pool, c.TrackID)
		if len(rows) != 1 {
			t.Fatalf("track %s: got %d rows, want 1", c.TrackID, len(rows))
		}
		got := rows[0]
		if got.outcome != string(c.Outcome) || got.reason != c.Reason || got.elapsedMs != c.ElapsedMs {
			t.Errorf("track %s: row = %+v, want outcome=%q reason=%q elapsedMs=%d", c.TrackID, got, c.Outcome, c.Reason, c.ElapsedMs)
		}
		if got.completedAt.Before(before) || got.completedAt.After(after) {
			t.Errorf("track %s: completed_at = %v, want between %v and %v", c.TrackID, got.completedAt, before, after)
		}
	}
}

func TestPgxOutcomeStore_RecordInsertsOneRowPerCall(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	for i := 0; i < 3; i++ {
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "succeeded", ElapsedMs: int64(i)}); err != nil {
			t.Fatalf("Record #%d = %v, want nil", i, err)
		}
	}

	rows := queryOutcomeRows(t, pool, trackID)
	if len(rows) != 3 {
		t.Fatalf("got %d rows for repeated Record calls, want 3", len(rows))
	}
}

func TestPgxOutcomeStore_RejectsOutcomeOutsideTheThreeStates(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "queued", ElapsedMs: 5})
	if err == nil {
		t.Fatal("Record with an unrecognised outcome = nil, want an error from the CHECK constraint")
	}

	rows := queryOutcomeRows(t, pool, trackID)
	if len(rows) != 0 {
		t.Fatalf("rejected Record still inserted %d rows, want 0", len(rows))
	}
}

func TestPgxOutcomeStore_RejectsNearMissOutcomeSpellings(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()

	for _, outcome := range []string{"", "Succeeded", "FAILED", "canceled", " succeeded", "succeeded "} {
		trackID := uuid.NewString()
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: ports.JobState(outcome), ElapsedMs: 1}); err == nil {
			t.Errorf("Record(outcome=%q) = nil, want an error", outcome)
		}
		if rows := queryOutcomeRows(t, pool, trackID); len(rows) != 0 {
			t.Errorf("Record(outcome=%q) left %d rows, want 0", outcome, len(rows))
		}
	}
}

func TestPgxOutcomeStore_StoresLongAndHostileReasonVerbatim(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()

	long := strings.Repeat("no_match_found; ", 8192)
	hostile := "it's \"quoted\"'); DROP TABLE acquisition_outcomes; -- 曲 🎵 \\n\t"
	for _, reason := range []string{long, hostile} {
		trackID := uuid.NewString()
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "failed", Reason: reason, ElapsedMs: 7}); err != nil {
			t.Fatalf("Record(reason of %d bytes) = %v, want nil", len(reason), err)
		}
		rows := queryOutcomeRows(t, pool, trackID)
		if len(rows) != 1 {
			t.Fatalf("Record(reason of %d bytes): got %d rows, want 1", len(reason), len(rows))
		}
		if rows[0].reason != reason {
			t.Errorf("stored reason of %d bytes differs from the %d bytes given", len(rows[0].reason), len(reason))
		}
	}
}

func TestPgxOutcomeStore_StoresElapsedAtZeroAndInt64Max(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()

	for _, elapsed := range []int64{0, 9223372036854775807} {
		trackID := uuid.NewString()
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "succeeded", ElapsedMs: elapsed}); err != nil {
			t.Fatalf("Record(elapsedMs=%d) = %v, want nil", elapsed, err)
		}
		rows := queryOutcomeRows(t, pool, trackID)
		if len(rows) != 1 || rows[0].elapsedMs != elapsed {
			t.Errorf("Record(elapsedMs=%d): rows = %+v, want one row with elapsedMs=%d", elapsed, rows, elapsed)
		}
	}
}

func TestPgxOutcomeStore_CancelledContextReturnsErrorAndWritesNothing(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trackID := uuid.NewString()

	err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "succeeded", ElapsedMs: 3})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Record with a cancelled context = %v, want an error wrapping context.Canceled", err)
	}
	if rows := queryOutcomeRows(t, pool, trackID); len(rows) != 0 {
		t.Errorf("Record with a cancelled context left %d rows, want 0", len(rows))
	}
}

func TestPgxOutcomeStore_ClosedPoolReturnsError(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	pool.Close()

	err := store.Record(context.Background(), ports.AcquisitionOutcome{TrackID: uuid.NewString(), Outcome: "failed", ElapsedMs: 1})
	if err == nil {
		t.Error("Record on a closed pool = nil, want an error")
	}
}

func TestPgxOutcomeStore_RejectsNegativeElapsedMs(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "succeeded", ElapsedMs: -1})
	if err == nil {
		t.Fatal("Record with elapsedMs=-1 = nil, want an error from the CHECK constraint")
	}

	rows := queryOutcomeRows(t, pool, trackID)
	if len(rows) != 0 {
		t.Fatalf("rejected Record still inserted %d rows, want 0", len(rows))
	}
}

func TestPgxOutcomeStore_RejectsEmptyTrackID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()

	err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: "", Outcome: "succeeded", ElapsedMs: 1})
	if err == nil {
		t.Fatal("Record with an empty TrackID = nil, want an error from the CHECK constraint")
	}

	rows := queryOutcomeRows(t, pool, "")
	if len(rows) != 0 {
		t.Fatalf("rejected Record still inserted %d rows, want 0", len(rows))
	}
}

func TestPgxOutcomeStore_ConcurrentRecordsEachLandOneRow(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	trackID := uuid.NewString()

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "succeeded", ElapsedMs: int64(i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Record = %v, want nil", err)
		}
	}

	if rows := queryOutcomeRows(t, pool, trackID); len(rows) != 20 {
		t.Errorf("20 concurrent Record calls left %d rows, want 20", len(rows))
	}
}

func TestPgxOutcomeStore_PruneDeletesOutcomesOlderThanRetentionAndKeepsNewer(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	oldTrack := insertFailedTrack(t, pool).ID.UUID().String()
	newTrack := insertFailedTrack(t, pool).ID.UUID().String()

	for _, seed := range []struct {
		trackID string
		age     string
	}{{oldTrack, "91 days"}, {newTrack, "89 days"}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms, completed_at) VALUES ($1, 'failed', 'no_match', 10, now() - $2::interval)`,
			seed.trackID, seed.age); err != nil {
			t.Fatalf("seed outcome: %v", err)
		}
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := queryOutcomeRows(t, pool, oldTrack); len(got) != 0 {
		t.Errorf("old outcomes after Prune = %v, want none", got)
	}
	if got := queryOutcomeRows(t, pool, newTrack); len(got) != 1 {
		t.Errorf("recent outcomes after Prune = %d, want 1", len(got))
	}
}

func TestPgxOutcomeStore_RecordsStageTimesAndFileFacts(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	trackID := uuid.NewString()

	err := store.Record(context.Background(), ports.AcquisitionOutcome{
		TrackID: trackID, Outcome: "succeeded", ElapsedMs: 9000,
		StageMs:    map[string]int64{"search": 800, "download.preview": 4100},
		SourceHost: "music.youtube.com",
		Source:     ports.SourceFormat{ID: "18", Codec: "mp4a.40.2"},
		Stored:     ports.StoredFile{Bytes: 7_340_032, Codec: "mp3"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var stageMs map[string]int64
	var host, formatID, sourceCodec, storedCodec string
	var kbps *int32
	var bytes int64
	err = pool.QueryRow(context.Background(), `
		SELECT stage_ms, source_host, source_format_id, source_codec, source_kbps, stored_bytes, stored_codec
		FROM acquisition_outcomes WHERE track_id = $1`, trackID).
		Scan(&stageMs, &host, &formatID, &sourceCodec, &kbps, &bytes, &storedCodec)
	if err != nil {
		t.Fatal(err)
	}
	if stageMs["search"] != 800 || stageMs["download.preview"] != 4100 || len(stageMs) != 2 {
		t.Errorf("stage_ms = %v, want exactly the two recorded stages", stageMs)
	}
	if host != "music.youtube.com" || formatID != "18" || sourceCodec != "mp4a.40.2" || bytes != 7_340_032 || storedCodec != "mp3" {
		t.Errorf("row = %s %s %s %d %s, want the recorded file facts", host, formatID, sourceCodec, bytes, storedCodec)
	}
	if kbps != nil {
		t.Errorf("source_kbps = %d, want NULL when yt-dlp reported no audio bitrate", *kbps)
	}
}

func TestPgxOutcomeStore_RecordsJobKindRunProvenanceAndLengths(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	full := uuid.NewString()
	bare := uuid.NewString()

	err := store.Record(ctx, ports.AcquisitionOutcome{
		TrackID: full, Outcome: "succeeded", Kind: ports.JobKindReplace, Run: 2, Provenance: "corroborated",
		Confidence: 0.87, SourceKey: "yt:abc", TargetSeconds: 215.5, MeasuredSeconds: 214.9,
	})
	if err != nil {
		t.Fatalf("Record full = %v, want nil", err)
	}
	if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: bare, Outcome: "failed"}); err != nil {
		t.Fatalf("Record bare = %v, want nil", err)
	}

	const q = `SELECT job_kind, run, provenance, confidence, source_key, target_seconds, measured_seconds
		FROM acquisition_outcomes WHERE track_id = $1`
	var kind, provenance, sourceKey string
	var run *int
	var confidence, target, measured *float64
	if err := pool.QueryRow(ctx, q, full).Scan(&kind, &run, &provenance, &confidence, &sourceKey, &target, &measured); err != nil {
		t.Fatalf("query full row: %v", err)
	}
	if kind != "replace" || run == nil || *run != 2 || provenance != "corroborated" || sourceKey != "yt:abc" ||
		confidence == nil || *confidence != 0.87 || target == nil || *target != 215.5 || measured == nil || *measured != 214.9 {
		t.Errorf("full row = %q %v %q %v %q %v %v, want replace 2 corroborated 0.87 yt:abc 215.5 214.9",
			kind, run, provenance, confidence, sourceKey, target, measured)
	}

	if err := pool.QueryRow(ctx, q, bare).Scan(&kind, &run, &provenance, &confidence, &sourceKey, &target, &measured); err != nil {
		t.Fatalf("query bare row: %v", err)
	}
	if kind != "" || run != nil || provenance != "" || confidence != nil || sourceKey != "" || target != nil || measured != nil {
		t.Errorf("bare row = %q %v %q %v %q %v %v, want empty text and NULL numbers", kind, run, provenance, confidence, sourceKey, target, measured)
	}
}

func TestPgxOutcomeStore_PruneRemovesRowsOfADeletedTrack(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	kept := insertFailedTrack(t, pool).ID.UUID().String()
	deleted := insertFailedTrack(t, pool).ID.UUID().String()
	for _, trackID := range []string{kept, deleted} {
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "failed", ElapsedMs: 10}); err != nil {
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

	if got := queryOutcomeRows(t, pool, deleted); len(got) != 0 {
		t.Errorf("outcomes of the deleted track after Prune = %v, want none", got)
	}
	if got := queryOutcomeRows(t, pool, kept); len(got) != 1 {
		t.Errorf("outcomes of the live track after Prune = %d, want 1", len(got))
	}
	if pruned < 1 {
		t.Errorf("Prune(...) = %d, want the orphan counted", pruned)
	}
}

func TestPgxOutcomeStore_PruneKeepsAnOutcomeStoredUnderTheUppercaseIDOfALiveTrack(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	upper := strings.ToUpper(live)
	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms, completed_at) VALUES ($1, 'failed', 'no_match', 10, now())`,
		upper); err != nil {
		t.Fatalf("seed outcome: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := queryOutcomeRows(t, pool, upper); len(got) != 1 {
		t.Errorf("outcomes under %q after Prune = %d, want 1 (its track %s is live)", upper, len(got), live)
	}
}

func TestPgxOutcomeStore_PruneSucceedsWhenATrackIDIsNotAUUID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	for _, trackID := range []string{"not-a-uuid-" + uuid.NewString(), live} {
		if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "failed", ElapsedMs: 10}); err != nil {
			t.Fatalf("Record(...) = %v, want nil", err)
		}
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) with a non-uuid track id stored = %v, want nil", err)
	}

	if got := queryOutcomeRows(t, pool, live); len(got) != 1 {
		t.Errorf("outcomes of the live track after Prune = %d, want 1", len(got))
	}
}

func TestPgxOutcomeStore_PruneKeepsRowsOfATrackDeletedAndRecreatedWithTheSameID(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	track := insertFailedTrack(t, pool)
	trackID := track.ID.UUID().String()
	if err := store.Record(ctx, ports.AcquisitionOutcome{TrackID: trackID, Outcome: "failed", ElapsedMs: 10}); err != nil {
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

	if got := queryOutcomeRows(t, pool, trackID); len(got) != 1 {
		t.Errorf("outcomes of the re-created track after Prune = %d, want 1", len(got))
	}
}

func TestPgxOutcomeStore_PruneRemovesOrphansFillingExactlyTwoBatchesAndKeepsLiveRows(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	live := insertFailedTrack(t, pool).ID.UUID().String()
	deleted := insertFailedTrack(t, pool)
	deletedID := deleted.ID.UUID().String()
	for _, trackID := range []string{live, deletedID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms, completed_at)
			 SELECT $1, 'failed', 'no_match', 10, now() FROM generate_series(1, $2) g`,
			trackID, 2*pruneBatchSize); err != nil {
			t.Fatalf("seed outcomes: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, deleted.ID.UUID()); err != nil {
		t.Fatalf("delete track: %v", err)
	}

	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if got := queryOutcomeRows(t, pool, deletedID); len(got) != 0 {
		t.Errorf("outcomes of the deleted track after Prune = %d, want 0", len(got))
	}
	if got := queryOutcomeRows(t, pool, live); len(got) != 2*pruneBatchSize {
		t.Errorf("outcomes of the live track after Prune = %d, want %d", len(got), 2*pruneBatchSize)
	}
}

func TestPgxOutcomeStore_PruneCountsAnOrphanOlderThanRetentionOnce(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxOutcomeStore(pool)
	ctx := context.Background()
	if _, err := store.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("draining Prune(...) = %v, want nil", err)
	}
	live := insertFailedTrack(t, pool).ID.UUID().String()
	orphan := uuid.NewString()
	for _, seed := range []struct {
		trackID string
		age     string
	}{
		{live, "91 days"},
		{live, "200 days"},
		{live, "1 day"},
		{orphan, "1 day"},
		{orphan, "2 days"},
		{orphan, "91 days"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms, completed_at) VALUES ($1, 'failed', 'no_match', 10, now() - $2::interval)`,
			seed.trackID, seed.age); err != nil {
			t.Fatalf("seed outcome: %v", err)
		}
	}

	pruned, err := store.Prune(ctx, time.Now())
	if err != nil {
		t.Fatalf("Prune(...) = %v, want nil", err)
	}

	if pruned != 5 {
		t.Errorf("Prune(...) = %d, want 5 (2 expired live rows + 3 orphan rows, one of them also expired)", pruned)
	}
	if got := queryOutcomeRows(t, pool, orphan); len(got) != 0 {
		t.Errorf("orphan outcomes after Prune = %d, want 0", len(got))
	}
	if got := queryOutcomeRows(t, pool, live); len(got) != 1 {
		t.Errorf("live track outcomes after Prune = %d, want 1", len(got))
	}
}
