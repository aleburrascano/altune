//go:build integration

package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeleteForUser_ErasedRowEqualsEmptyQueueState(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, pool, userId)

	populated := &domain.QueueState{
		UserId:       userId,
		TrackIds:     []string{"a", "b", "c"},
		CurrentIdx:   1,
		PositionMs:   42000,
		Shuffled:     true,
		RepeatMode:   domain.RepeatAll,
		SourceId:     "playlist-1",
		NaturalOrder: []string{"c", "a", "b"},
		UpdatedAt:    time.Now().UTC(),
	}
	if err := repo.Upsert(ctx, populated); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.DeleteForUser(ctx, userId); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	var got domain.QueueState
	var repeatMode string
	err := pool.QueryRow(ctx,
		`SELECT track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order
		 FROM playback_queue_state WHERE user_id = $1`, userId.UUID(),
	).Scan(&got.TrackIds, &got.CurrentIdx, &got.PositionMs, &got.Shuffled,
		&repeatMode, &got.SourceId, &got.NaturalOrder)
	if err != nil {
		t.Fatalf("read erased row: %v", err)
	}

	want := *domain.EmptyQueueState(userId)
	if repeatMode != want.RepeatMode.String() {
		t.Errorf("repeat_mode = %q, want %q", repeatMode, want.RepeatMode.String())
	}
	got.UserId, got.RepeatMode, got.UpdatedAt = want.UserId, want.RepeatMode, want.UpdatedAt
	if !reflect.DeepEqual(got, want) {
		t.Errorf("erased row = %+v, want %+v", got, want)
	}
}
