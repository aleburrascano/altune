//go:build integration

package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
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
