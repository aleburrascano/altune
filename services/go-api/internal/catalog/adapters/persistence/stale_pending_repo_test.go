package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPgxTrackRepo_FailStalePending proves the durable in-flight marker survives a
// round-trip and that the sweep fails only tracks older than the cutoff, leaving a
// freshly scheduled (still legitimately in-flight) track pending.
func TestPgxTrackRepo_FailStalePending(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	stale := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, stale.ID, userId)
	if _, _, err := repo.Add(ctx, stale); err != nil {
		t.Fatalf("Add stale: %v", err)
	}

	fresh := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, fresh.ID, userId)
	if _, _, err := repo.Add(ctx, fresh); err != nil {
		t.Fatalf("Add fresh: %v", err)
	}

	// The marker must round-trip: a pending track carries its in-flight timestamp.
	gotStale, err := repo.GetByID(ctx, stale.ID, userId)
	if err != nil || gotStale == nil {
		t.Fatalf("GetByID stale: track=%v err=%v", gotStale, err)
	}
	if gotStale.AcquisitionStartedAt == nil {
		t.Fatal("acquisition_started_at did not round-trip; marker is nil")
	}

	// Backdate the stale track's marker to well before the cutoff.
	if _, err := pool.Exec(ctx,
		`UPDATE tracks SET acquisition_started_at = $2 WHERE id = $1`,
		stale.ID.UUID(), time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("backdate marker: %v", err)
	}

	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	n, err := repo.FailStalePending(ctx, cutoff, domain.ReasonAcquisitionInterrupted)
	if err != nil {
		t.Fatalf("FailStalePending: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d tracks, want 1 (the backdated one only)", n)
	}

	healed, err := repo.GetByID(ctx, stale.ID, userId)
	if err != nil || healed == nil {
		t.Fatalf("GetByID after sweep: track=%v err=%v", healed, err)
	}
	if healed.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("stale track status = %v, want failed", healed.AcquisitionStatus)
	}
	if healed.FailureReason == nil || *healed.FailureReason != domain.ReasonAcquisitionInterrupted {
		t.Errorf("failure reason = %v, want %q", healed.FailureReason, domain.ReasonAcquisitionInterrupted)
	}
	if healed.AcquisitionStartedAt != nil {
		t.Errorf("marker = %v, want cleared after sweep", healed.AcquisitionStartedAt)
	}

	stillFresh, err := repo.GetByID(ctx, fresh.ID, userId)
	if err != nil || stillFresh == nil {
		t.Fatalf("GetByID fresh after sweep: track=%v err=%v", stillFresh, err)
	}
	if stillFresh.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("fresh track status = %v, want still pending", stillFresh.AcquisitionStatus)
	}
}
