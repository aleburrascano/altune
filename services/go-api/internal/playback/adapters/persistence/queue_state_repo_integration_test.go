//go:build integration

package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// singleConnPool is a pool of exactly one connection, so a save started while
// that connection is held waits in the pool instead of reaching Postgres: the
// delay a stale write needs to be reproduced deterministically.
func singleConnPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// dropQueueStateOnCleanup removes every trace of a test user: the stored queue,
// or the blanked row an erasure leaves in its place.
func dropQueueStateOnCleanup(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})
}

func stateAt(userId shared.UserId, positionMs int64, at time.Time) *domain.QueueState {
	return &domain.QueueState{
		UserId:       userId,
		TrackIds:     []string{},
		NaturalOrder: []string{},
		PositionMs:   positionMs,
		RepeatMode:   domain.RepeatOff,
		UpdatedAt:    at,
	}
}

func TestDeleteForUser_ErasesPersistedRow(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	dropQueueStateOnCleanup(t, pool, userId)

	if err := repo.Upsert(ctx, stateAt(userId, 42000, time.Now().UTC())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err := repo.GetForUser(ctx, userId); err != nil || got == nil {
		t.Fatalf("precondition: state must exist before deletion (got=%v err=%v)", got, err)
	}

	if err := repo.DeleteForUser(ctx, userId); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("row survived DeleteForUser: %+v", got)
	}
}

func TestDeleteForUser_MissingRowIsNotAnError(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)

	if err := repo.DeleteForUser(context.Background(), shared.NewUserId(uuid.New())); err != nil {
		t.Fatalf("deleting a user with no stored state must be a no-op, got: %v", err)
	}
}

func TestUpsert_OlderSnapshotDoesNotClobberNewer(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	dropQueueStateOnCleanup(t, pool, userId)

	now := time.Now().UTC()
	newer := stateAt(userId, 65000, now)
	older := stateAt(userId, 60000, now.Add(-5*time.Second))

	if err := repo.Upsert(ctx, newer); err != nil {
		t.Fatalf("Upsert(newer): %v", err)
	}
	if err := repo.Upsert(ctx, older); !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(older) = %v, want ErrStaleQueueWrite; a rejected write must not report success", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil {
		t.Fatal("GetForUser returned nil after upsert")
	}
	if got.PositionMs != 65000 {
		t.Fatalf("position_ms = %d, want 65000; a delayed older save reverted the newer snapshot", got.PositionMs)
	}
}

func TestUpsert_NewerSnapshotStillApplies(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	dropQueueStateOnCleanup(t, pool, userId)

	now := time.Now().UTC()
	if err := repo.Upsert(ctx, stateAt(userId, 60000, now.Add(-5*time.Second))); err != nil {
		t.Fatalf("Upsert(older): %v", err)
	}
	if err := repo.Upsert(ctx, stateAt(userId, 65000, now)); err != nil {
		t.Fatalf("Upsert(newer): %v", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil {
		t.Fatal("GetForUser returned nil after upsert")
	}
	if got.PositionMs != 65000 {
		t.Fatalf("position_ms = %d, want 65000; the newer snapshot did not apply", got.PositionMs)
	}
}

// TestUpsert_FastInstanceClockDoesNotRejectLaterSave reproduces the
// wall-clock ordering defect (#1121): save A is handled first by an instance
// whose clock runs 10s fast, save B is handled after it by an accurate one. B
// is the logically newer snapshot, but ordering by each process's own
// time.Now() made A look newer, so B was rejected and the older A won.
func TestUpsert_FastInstanceClockDoesNotRejectLaterSave(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	dropQueueStateOnCleanup(t, pool, userId)

	fastInstance := stateAt(userId, 60000, time.Now().UTC().Add(10*time.Second))
	if err := repo.Upsert(ctx, fastInstance); err != nil {
		t.Fatalf("Upsert(A, fast clock): %v", err)
	}
	accurateInstance := stateAt(userId, 65000, time.Now())
	if err := repo.Upsert(ctx, accurateInstance); err != nil {
		t.Fatalf("Upsert(B, handled after A) = %v, want nil; a skewed instance clock rejected the newer save", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil || got.PositionMs != 65000 {
		t.Fatalf("stored state = %+v, want position_ms 65000; the older save won", got)
	}
}

// TestUpsert_SaveDelayedInPoolIsStillStale pins the 409 contract for a truly
// stale write: save A is stamped first but waits for a pooled connection while
// the later save B commits. A must be rejected, not land last and clobber B.
func TestUpsert_SaveDelayedInPoolIsStillStale(t *testing.T) {
	wide := testPool(t)
	narrow := singleConnPool(t)

	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, wide, userId)

	held, err := narrow.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	older := stateAt(userId, 60000, time.Now())
	done := make(chan error, 1)
	go func() { done <- NewPgxQueueStateRepository(narrow).Upsert(ctx, older) }()

	time.Sleep(300 * time.Millisecond)
	newerErr := NewPgxQueueStateRepository(wide).Upsert(ctx, stateAt(userId, 65000, time.Now()))
	held.Release()
	if newerErr != nil {
		t.Fatalf("Upsert(newer): %v", newerErr)
	}

	if err := <-done; !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(older, delayed in pool) = %v, want ErrStaleQueueWrite", err)
	}
	got, err := NewPgxQueueStateRepository(wide).GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil || got.PositionMs != 65000 {
		t.Fatalf("stored state = %+v, want position_ms 65000; the delayed older save clobbered the newer one", got)
	}
}

// TestUpsert_SaveDelayedPastAnErasureDoesNotResurrectIt reproduces #1594: an
// autosave stamped before the user erased their queue, but still waiting for a
// pooled connection when the erasure committed, found no row to conflict with
// and so landed as a plain INSERT — silently recreating the track list, natural
// order and free-text source_id the user had asked to be forgotten.
func TestUpsert_SaveDelayedPastAnErasureDoesNotResurrectIt(t *testing.T) {
	wide := testPool(t)
	narrow := singleConnPool(t)

	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, wide, userId)

	if err := NewPgxQueueStateRepository(wide).Upsert(ctx, stateAt(userId, 60000, time.Now())); err != nil {
		t.Fatalf("Upsert(the queue the user then erases): %v", err)
	}
	held, err := narrow.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	inFlight := stateAt(userId, 65000, time.Now())
	done := make(chan error, 1)
	go func() { done <- NewPgxQueueStateRepository(narrow).Upsert(ctx, inFlight) }()

	time.Sleep(300 * time.Millisecond)
	forgetErr := NewPgxQueueStateRepository(wide).DeleteForUser(ctx, userId)
	held.Release()
	if forgetErr != nil {
		t.Fatalf("DeleteForUser: %v", forgetErr)
	}

	if err := <-done; !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(handled before the erasure, executed after it) = %v, want ErrStaleQueueWrite", err)
	}
	got, err := NewPgxQueueStateRepository(wide).GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got != nil {
		t.Fatalf("erased queue state came back: %+v; a save handled before the erasure recreated the row", got)
	}
}

// TestUpsert_SaveBlockedOnTheErasureLockDoesNotResurrectIt is the same defect
// under true concurrency, where the delay is the erasure's own row lock: the
// save is already executing when the erasure commits, so its snapshot predates
// the erasure and nothing it reads separately could reveal it. Only the row the
// erasure writes, which the save must re-check its guard against before it can
// write over it, decides this one.
func TestUpsert_SaveBlockedOnTheErasureLockDoesNotResurrectIt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, pool, userId)

	if err := NewPgxQueueStateRepository(pool).Upsert(ctx, stateAt(userId, 60000, time.Now())); err != nil {
		t.Fatalf("Upsert(the queue the user then erases): %v", err)
	}
	inFlight := stateAt(userId, 65000, time.Now())

	erasure, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	erasingRepo := &PgxQueueStateRepository{pool: erasure, metrics: ports.NoopQueueStateMetrics()}
	if err := erasingRepo.DeleteForUser(ctx, userId); err != nil {
		t.Fatalf("DeleteForUser(holding the row): %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- NewPgxQueueStateRepository(pool).Upsert(ctx, inFlight) }()
	time.Sleep(300 * time.Millisecond)

	if err := erasure.Commit(ctx); err != nil {
		t.Fatalf("commit the erasure: %v", err)
	}

	if err := <-done; !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(blocked on the erasure's lock) = %v, want ErrStaleQueueWrite", err)
	}
	got, err := NewPgxQueueStateRepository(pool).GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got != nil {
		t.Fatalf("erased queue state came back: %+v; the save that waited out the erasure recreated it", got)
	}
}

// TestDeleteForUser_ReapsRowsErasedPastTheFenceWindow pins the bound on the
// rows an erasure leaves behind: once no save older than one can still be in
// flight it decides nothing, and keeping it would leave the identifier of an
// account that may itself be deleted lying in the table for good.
func TestDeleteForUser_ReapsRowsErasedPastTheFenceWindow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	longErased, erasingNow := shared.NewUserId(uuid.New()), shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, pool, longErased)
	dropQueueStateOnCleanup(t, pool, erasingNow)

	_, err := pool.Exec(ctx,
		`INSERT INTO playback_queue_state (user_id, updated_at, erased_at)
		 VALUES ($1, clock_timestamp(), clock_timestamp() - $2::bigint * interval '1 second')`,
		longErased.UUID(), int64(erasureFenceWindow.Seconds())+60)
	if err != nil {
		t.Fatalf("seed a long-erased row: %v", err)
	}

	if err := NewPgxQueueStateRepository(pool).DeleteForUser(ctx, erasingNow); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM playback_queue_state WHERE user_id = $1`,
		longErased.UUID()).Scan(&remaining); err != nil {
		t.Fatalf("count erased rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("long-erased row survived (%d rows); erased rows grow without bound", remaining)
	}
}

// TestUpsert_SaveHandledAfterAnErasureStillApplies holds the other half of the
// fence: erasure rejects the writes already in flight, it does not lock the
// user out of saving again.
func TestUpsert_SaveHandledAfterAnErasureStillApplies(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	dropQueueStateOnCleanup(t, pool, userId)

	if err := repo.Upsert(ctx, stateAt(userId, 60000, time.Now())); err != nil {
		t.Fatalf("Upsert(before the erasure): %v", err)
	}
	if err := repo.DeleteForUser(ctx, userId); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	if err := repo.Upsert(ctx, stateAt(userId, 1000, time.Now())); err != nil {
		t.Fatalf("Upsert(handled after the erasure) = %v, want nil; erasure is not a lockout", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil || got.PositionMs != 1000 {
		t.Fatalf("stored state = %+v, want position_ms 1000; the post-erasure save did not apply", got)
	}
}

// TestUpdatePosition_DelayedPastAnErasureDoesNotRecreateTheRow pins the other
// write path against the same race. It updates and never inserts, so it cannot
// resurrect a queue; what it must not do is report success for a row that is
// no longer there.
func TestUpdatePosition_DelayedPastAnErasureDoesNotRecreateTheRow(t *testing.T) {
	wide := testPool(t)
	narrow := singleConnPool(t)

	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	dropQueueStateOnCleanup(t, wide, userId)

	seeded := stateAt(userId, 60000, time.Now())
	seeded.TrackIds = []string{"track-a"}
	seeded.NaturalOrder = []string{"track-a"}
	if err := NewPgxQueueStateRepository(wide).Upsert(ctx, seeded); err != nil {
		t.Fatalf("Upsert(the queue the user then erases): %v", err)
	}
	position, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId: userId, CurrentIdx: 0, CurrentTrackId: "track-a", PositionMs: 65000,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	held, err := narrow.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- NewPgxQueueStateRepository(narrow).UpdatePosition(ctx, position) }()

	time.Sleep(300 * time.Millisecond)
	forgetErr := NewPgxQueueStateRepository(wide).DeleteForUser(ctx, userId)
	held.Release()
	if forgetErr != nil {
		t.Fatalf("DeleteForUser: %v", forgetErr)
	}

	if err := <-done; !errors.Is(err, domain.ErrQueuePositionMismatch) {
		t.Fatalf("UpdatePosition(executed after the erasure) = %v, want ErrQueuePositionMismatch", err)
	}
	got, err := NewPgxQueueStateRepository(wide).GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got != nil {
		t.Fatalf("erased queue state came back: %+v", got)
	}
}

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

// uncommittedFullSave writes a full save from its own transaction and leaves it
// open, so the row stays locked and the next writer to it must wait. Calling
// the returned commit ends the wait with the save committed.
func uncommittedFullSave(t *testing.T, ctx context.Context, pool *pgxpool.Pool, state *domain.QueueState) (commit func()) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if err := (&PgxQueueStateRepository{pool: tx, metrics: ports.NoopQueueStateMetrics()}).Upsert(ctx, state); err != nil {
		t.Fatalf("Upsert(concurrent full save): %v", err)
	}
	return func() {
		if err := tx.Commit(ctx); err != nil {
			t.Errorf("commit the concurrent full save: %v", err)
		}
	}
}

// awaitBlockedQueueWriter blocks until another backend is waiting on a lock
// over playback_queue_state. Without it a test only proves the two saves ran in
// some order, not that one resolved its write against a row the other changed
// under it, which is the whole contended window.
func awaitBlockedQueueWriter(t *testing.T, ctx context.Context, q querier) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := q.QueryRow(ctx,
			`SELECT EXISTS (
			   SELECT 1 FROM pg_stat_activity
			   WHERE pid <> pg_backend_pid()
			     AND wait_event_type = 'Lock'
			     AND query LIKE '%playback_queue_state%'
			 )`).Scan(&blocked); err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no writer ever waited on the queue row; the contended write was not reproduced")
}

// TestUpdatePosition_QueueReplacedDuringItsLockWaitTellsTheClientToResync pins
// the 409 subtype under a real race (#1570): the position save waits on the row
// lock a concurrent full save holds, and that save moves the track the position
// was measured against. Both subtypes are 409 and neither writes, but the
// client acts on the difference — ErrStaleQueueWrite says retry, and retrying a
// position measured against a queue that no longer exists can only fail again.
func TestUpdatePosition_QueueReplacedDuringItsLockWaitTellsTheClientToResync(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, []string{"a", "b", "c", "d", "e", "f"}, 1000)); err != nil {
		t.Fatalf("Upsert(the queue the position is measured against): %v", err)
	}
	commitReplacement := uncommittedFullSave(t, ctx, pool, maxLengthQueue(t, userId, []string{"x", "y", "z", "w", "v", "u"}, 4000))
	measuredOnTheOldQueue := positionAt(t, userId, 1, "b", 99000, 0)

	raced := make(chan error, 1)
	go func() { raced <- repo.UpdatePosition(ctx, measuredOnTheOldQueue) }()
	awaitBlockedQueueWriter(t, ctx, pool)
	commitReplacement()

	if err := <-raced; !errors.Is(err, domain.ErrQueuePositionMismatch) {
		t.Fatalf("UpdatePosition that lost its track while waiting on the lock = %v, want ErrQueuePositionMismatch", err)
	}
	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil || got.PositionMs != 4000 || got.CurrentIdx != 5 || got.TrackIds[1] != "y" {
		t.Fatalf("stored state = %+v (err %v), want the replacement queue untouched", got, err)
	}
}

// TestUpdatePosition_AppliesOverAFullSaveItWaitedOut is the other arm of the
// same race: the full save committed during the wait keeps the track at the
// index and was handled first, so the position belongs on it and must land.
func TestUpdatePosition_AppliesOverAFullSaveItWaitedOut(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	ids := []string{"a", "b", "c", "d", "e", "f"}

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, ids, 1000)); err != nil {
		t.Fatalf("Upsert(seed): %v", err)
	}
	commitSameQueue := uncommittedFullSave(t, ctx, pool, maxLengthQueue(t, userId, ids, 4000))
	position := positionAt(t, userId, 1, "b", 99000, 0)

	raced := make(chan error, 1)
	go func() { raced <- repo.UpdatePosition(ctx, position) }()
	awaitBlockedQueueWriter(t, ctx, pool)
	commitSameQueue()

	if err := <-raced; err != nil {
		t.Fatalf("UpdatePosition over a full save it waited out = %v, want nil", err)
	}
	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil || got.PositionMs != 99000 || got.CurrentIdx != 1 {
		t.Fatalf("stored state = %+v (err %v), want the position that waited out the full save", got, err)
	}
}

// TestUpdatePosition_HandledBeforeTheFullSaveItWaitedOutStaysStale is the
// lost-update arm of the same race (#1578): same contention, only the handling
// order reversed, so the position must be rejected and the full save's data
// survive. A save's own instant is read from the database clock inside the
// statement, and the FOR UPDATE lock #1570 added put that read after the wait
// on a concurrent writer — so waiting long enough aged an older save past the
// newer full save it was waiting for, the ordering guard passed, and the full
// save's position was overwritten by data measured before it.
func TestUpdatePosition_HandledBeforeTheFullSaveItWaitedOutStaysStale(t *testing.T) {
	// The gap decides which save is newer; the hold is how far a clock read
	// taken after the wait would drift. The hold exceeds the gap by enough that
	// a scheduling stall cannot swap the two.
	const (
		handledBeforeTheFullSave = 250 * time.Millisecond
		lockHeldWhileItWaits     = time.Second
	)
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupUser(t, pool, userId)
	ids := []string{"a", "b", "c", "d", "e", "f"}

	if err := repo.Upsert(ctx, maxLengthQueue(t, userId, ids, 1000)); err != nil {
		t.Fatalf("Upsert(seed): %v", err)
	}
	commitNewerFull := uncommittedFullSave(t, ctx, pool, maxLengthQueue(t, userId, ids, 4000))
	olderPosition := positionAt(t, userId, 1, "b", 99000, handledBeforeTheFullSave)

	raced := make(chan error, 1)
	go func() { raced <- repo.UpdatePosition(ctx, olderPosition) }()
	awaitBlockedQueueWriter(t, ctx, pool)
	time.Sleep(lockHeldWhileItWaits)
	commitNewerFull()

	if err := <-raced; !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("UpdatePosition(handled before the full save it waited out) = %v, want ErrStaleQueueWrite", err)
	}
	got, err := repo.GetForUser(ctx, userId)
	if err != nil || got == nil || got.PositionMs != 4000 || got.CurrentIdx != 5 {
		t.Fatalf("stored state = %+v (err %v), want the newer full save's position 4000 at index 5; the lock wait aged the older position save past it", got, err)
	}
}
