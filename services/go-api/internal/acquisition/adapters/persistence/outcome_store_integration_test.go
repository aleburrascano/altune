package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/sharedtest"
	"context"
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
		if got.outcome != c.Outcome || got.reason != c.Reason || got.elapsedMs != c.ElapsedMs {
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
