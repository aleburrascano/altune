//go:build integration

package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// maxUnchangedListWALBytes bounds the WAL a save may generate when neither
// stored track list changes. Rewriting a max-length queue's two TEXT[] columns
// costs ~880 KiB of WAL; a row header plus the scalar columns is a few hundred
// bytes, so 16 KiB separates the two by two orders of magnitude while leaving
// room for a full-page image on the first touch after a checkpoint.
const maxUnchangedListWALBytes = 16 << 10

// walInsertLSN reads the current WAL insert position. The difference between
// two readings around a statement is the WAL that statement generated (plus
// any concurrent activity, which a dedicated test database does not have).
func walInsertLSN(t *testing.T, ctx context.Context, q querier) string {
	t.Helper()
	var lsn string
	if err := q.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn); err != nil {
		t.Fatalf("read WAL position: %v", err)
	}
	return lsn
}

func walBytesSince(t *testing.T, ctx context.Context, q querier, lsn string) int64 {
	t.Helper()
	var n int64
	if err := q.QueryRow(ctx,
		`SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::pg_lsn)::bigint`, lsn,
	).Scan(&n); err != nil {
		t.Fatalf("measure WAL: %v", err)
	}
	return n
}

func maxLengthQueue(t *testing.T, userId shared.UserId, trackIds []string, positionMs int64) *domain.QueueState {
	t.Helper()
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:       userId,
		TrackIds:     trackIds,
		NaturalOrder: trackIds,
		CurrentIdx:   5,
		PositionMs:   positionMs,
		RepeatMode:   domain.RepeatOff,
		SourceId:     "library",
	})
	if err != nil {
		t.Fatalf("NewQueueState: %v", err)
	}
	return state
}

func uuidTrackIds(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	return ids
}

func cleanupUser(t *testing.T, q querier, userId shared.UserId) {
	t.Cleanup(func() {
		_, _ = q.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})
}

// TestUpsert_UnchangedTrackListsAreNotRewritten measures the write a periodic
// autosave pays on a max-length queue when only the position moved (#1126).
// Every shipped client sends the full queue on each save, so the full-state
// path itself must not rewrite the two large arrays when they are unchanged.
func TestUpsert_UnchangedTrackListsAreNotRewritten(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	trackIds := uuidTrackIds(domain.MaxQueueLength)

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, trackIds, 1000)); err != nil {
		t.Fatalf("Upsert(initial): %v", err)
	}

	lsn := walInsertLSN(t, ctx, pool)
	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, trackIds, 16000)); err != nil {
		t.Fatalf("Upsert(position moved): %v", err)
	}
	wal := walBytesSince(t, ctx, pool, lsn)
	t.Logf("full-state save, unchanged %d-track lists: %d WAL bytes", domain.MaxQueueLength, wal)
	if wal > maxUnchangedListWALBytes {
		t.Fatalf("a save whose track lists did not change wrote %d WAL bytes, want <= %d; the unchanged arrays were rewritten", wal, maxUnchangedListWALBytes)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil {
		t.Fatalf("GetForUser: state=%v err=%v", got, err)
	}
	if got.PositionMs != 16000 || len(got.TrackIds) != domain.MaxQueueLength || got.TrackIds[5] != trackIds[5] ||
		len(got.NaturalOrder) != domain.MaxQueueLength || got.NaturalOrder[9999] != trackIds[9999] {
		t.Fatalf("stored state after position save is wrong: position=%d tracks=%d natural=%d", got.PositionMs, len(got.TrackIds), len(got.NaturalOrder))
	}
}

// TestUpsert_ChangedTrackListIsStillWritten keeps the full-state contract: a
// save that does carry a new track list replaces the stored one.
func TestUpsert_ChangedTrackListIsStillWritten(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, uuidTrackIds(domain.MaxQueueLength), 1000)); err != nil {
		t.Fatalf("Upsert(initial): %v", err)
	}
	replacement := []string{"x", "y", "z", "w", "v", "u"}
	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, replacement, 0)); err != nil {
		t.Fatalf("Upsert(replacement): %v", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil {
		t.Fatalf("GetForUser: state=%v err=%v", got, err)
	}
	if len(got.TrackIds) != len(replacement) || got.TrackIds[5] != "u" || len(got.NaturalOrder) != len(replacement) {
		t.Fatalf("replacement list not stored: tracks=%v natural=%v", got.TrackIds, got.NaturalOrder)
	}
}

func positionAt(t *testing.T, userId shared.UserId, idx int, trackId string, positionMs int64, handledAgo time.Duration) *domain.QueuePosition {
	t.Helper()
	p, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId: userId, CurrentIdx: idx, CurrentTrackId: trackId, PositionMs: positionMs,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	p.UpdatedAt = p.UpdatedAt.Add(-handledAgo)
	return p
}

// TestUpdatePosition_WritesOnlyThePosition measures the lighter path on a
// max-length queue: a few hundred bytes of WAL, lists intact, and the stored
// row still rehydrates with the moved index and position.
func TestUpdatePosition_WritesOnlyThePosition(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	trackIds := uuidTrackIds(domain.MaxQueueLength)

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, trackIds, 1000)); err != nil {
		t.Fatalf("Upsert(initial): %v", err)
	}

	lsn := walInsertLSN(t, ctx, pool)
	if err := repo.UpdatePosition(ctx, positionAt(t, userId, 9999, trackIds[9999], 16000, 0)); err != nil {
		t.Fatalf("UpdatePosition: %v", err)
	}
	wal := walBytesSince(t, ctx, pool, lsn)
	t.Logf("position-only save, %d-track queue: %d WAL bytes", domain.MaxQueueLength, wal)
	if wal > maxUnchangedListWALBytes {
		t.Fatalf("a position-only save wrote %d WAL bytes, want <= %d", wal, maxUnchangedListWALBytes)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil {
		t.Fatalf("GetForUser: state=%v err=%v", got, err)
	}
	if got.CurrentIdx != 9999 || got.PositionMs != 16000 {
		t.Fatalf("position not stored: idx=%d position=%d", got.CurrentIdx, got.PositionMs)
	}
	if len(got.TrackIds) != domain.MaxQueueLength || got.TrackIds[0] != trackIds[0] ||
		len(got.NaturalOrder) != domain.MaxQueueLength || got.SourceId != "library" {
		t.Fatalf("a position-only save disturbed the queue: tracks=%d natural=%d source=%q", len(got.TrackIds), len(got.NaturalOrder), got.SourceId)
	}
}

// TestUpdatePosition_NeverOverwritesANewerFullQueue pins the stale-write
// contract (#664) for the position path: a position handled before a newer
// full save is rejected, and the newer queue and its position survive.
func TestUpdatePosition_NeverOverwritesANewerFullQueue(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	ids := []string{"a", "b", "c", "d", "e", "f"}

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, ids, 65000)); err != nil {
		t.Fatalf("Upsert(newer full save): %v", err)
	}
	// Same queue, same track at the index: only the handling order says no.
	err := repo.UpdatePosition(ctx, positionAt(t, userId, 1, "b", 1, 5*time.Second))
	if !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("UpdatePosition(handled before the stored save) = %v, want ErrStaleQueueWrite", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil {
		t.Fatalf("GetForUser: state=%v err=%v", got, err)
	}
	if got.PositionMs != 65000 || got.CurrentIdx != 5 {
		t.Fatalf("a stale position save changed the newer queue: idx=%d position=%d", got.CurrentIdx, got.PositionMs)
	}
}

// TestUpdatePosition_OrdersAgainstFullSavesBothWays: a full save handled
// before a position save that already landed is itself the stale one.
func TestUpdatePosition_OrdersAgainstFullSavesBothWays(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	ids := []string{"a", "b", "c", "d", "e", "f"}

	if err := repo.Upsert(ctx, stateAt(userId, 0, time.Now().Add(-10*time.Second))); err != nil {
		t.Fatalf("Upsert(seed): %v", err)
	}
	delayedFull := maxLengthQueue(t, userId, ids, 1000)
	delayedFull.UpdatedAt = delayedFull.UpdatedAt.Add(-5 * time.Second)
	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, ids, 2000)); err != nil {
		t.Fatalf("Upsert(full): %v", err)
	}
	if err := repo.UpdatePosition(ctx, positionAt(t, userId, 5, "f", 30000, 0)); err != nil {
		t.Fatalf("UpdatePosition: %v", err)
	}
	if err := repo.Upsert(ctx, delayedFull); !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(handled before the position save) = %v, want ErrStaleQueueWrite", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil || got.PositionMs != 30000 {
		t.Fatalf("stored state = %+v (err %v), want position 30000 from the later position save", got, err)
	}
}

// TestUpdatePosition_DoesNotLandOnADifferentQueue: once another save replaced
// the queue, a position measured against the old one does not apply, and
// nothing is created for a user with no stored queue.
func TestUpdatePosition_DoesNotLandOnADifferentQueue(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)

	if err := repo.UpdatePosition(ctx, positionAt(t, userId, 0, "a", 1, 0)); !errors.Is(err, domain.ErrQueuePositionMismatch) {
		t.Fatalf("UpdatePosition with nothing stored = %v, want ErrQueuePositionMismatch", err)
	}
	if got, err := repo.GetForUser(ctx, userId); err != nil || got != nil {
		t.Fatalf("a position-only save created a row: state=%+v err=%v", got, err)
	}

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, []string{"x", "y", "z", "w", "v", "u"}, 4000)); err != nil {
		t.Fatalf("Upsert(replacement queue): %v", err)
	}
	for name, p := range map[string]*domain.QueuePosition{
		"different track at the index": positionAt(t, userId, 1, "b", 99000, 0),
		"index past the stored queue":  positionAt(t, userId, 6, "u", 99000, 0),
	} {
		if err := repo.UpdatePosition(ctx, p); !errors.Is(err, domain.ErrQueuePositionMismatch) {
			t.Fatalf("%s: UpdatePosition = %v, want ErrQueuePositionMismatch", name, err)
		}
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil || got.PositionMs != 4000 || got.CurrentIdx != 5 {
		t.Fatalf("stored state = %+v (err %v), want the replacement queue untouched", got, err)
	}
}
