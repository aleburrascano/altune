package persistence_test

import (
	"altune/go-api/internal/playback"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type storedQueueRow struct {
	trackIds     []string
	currentIdx   int
	shuffled     bool
	naturalOrder []string
	updatedAt    time.Time
}

func readStoredRow(t *testing.T, row storedQueueRow) (*playback.QueueState, error) {
	t.Helper()
	sharedtest.RequireIntegration(t)
	dbURL := sharedtest.DatabaseURL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	user := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_queue_state WHERE user_id = $1`, user.UUID())
	})
	if row.naturalOrder == nil {
		row.naturalOrder = []string{}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO playback_queue_state (user_id, track_ids, current_idx, shuffled, natural_order, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		user.UUID(), row.trackIds, row.currentIdx, row.shuffled, row.naturalOrder, row.updatedAt); err != nil {
		t.Fatalf("store row: %v", err)
	}
	return playback.NewPgxQueueStateRepository(pool).GetForUser(ctx, user)
}

func TestGetForUser_StoredRowWithCurrentIdxPastEndIsCorrupt(t *testing.T) {
	_, err := readStoredRow(t, storedQueueRow{trackIds: []string{"a", "b"}, currentIdx: 9, updatedAt: time.Now()})

	if !errors.Is(err, playback.ErrCorruptStoredState) {
		t.Fatalf("GetForUser = %v, want an out-of-range current_idx read as corrupt stored state", err)
	}
}

func TestGetForUser_KeepsTheStoredUpdatedAt(t *testing.T) {
	stored := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	state, err := readStoredRow(t, storedQueueRow{trackIds: []string{"a"}, updatedAt: stored})
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if !state.UpdatedAt.Equal(stored) {
		t.Errorf("UpdatedAt = %v, want %v (stored value, not now)", state.UpdatedAt, stored)
	}
}

func TestGetForUser_KeepsAStoredRowWithEmptyCurrentTrackId(t *testing.T) {
	state, err := readStoredRow(t, storedQueueRow{trackIds: []string{"a", ""}, currentIdx: 1, updatedAt: time.Now()})
	if err != nil {
		t.Fatalf("stored row must still load: %v", err)
	}
	if len(state.TrackIds) != 2 {
		t.Errorf("TrackIds = %v, want the stored queue intact", state.TrackIds)
	}
}

func TestGetForUser_LoadsALegacyNonCanonicalRowUnchanged(t *testing.T) {
	legacy := []string{"3F2504E0-4F89-11D3-9A0C-0305E82C3301", "not-a-uuid", ""}

	state, err := readStoredRow(t, storedQueueRow{trackIds: legacy, naturalOrder: []string{"x"}, shuffled: true, updatedAt: time.Now()})
	if err != nil {
		t.Fatalf("a legacy stored row must still load: %v", err)
	}
	if !slices.Equal(state.TrackIds, legacy) {
		t.Errorf("TrackIds = %q, want the stored ids untouched", state.TrackIds)
	}
	if err := state.Validate(); err != nil {
		t.Errorf("the pre-write check must not reject a legacy row: %v", err)
	}
}
