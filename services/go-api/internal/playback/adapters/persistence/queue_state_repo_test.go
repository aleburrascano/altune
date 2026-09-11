package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
)

type blockingQuerier struct{}

func (blockingQuerier) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	<-ctx.Done()
	return pgconn.CommandTag{}, ctx.Err()
}

func (blockingQuerier) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	<-ctx.Done()
	return errRow{err: ctx.Err()}
}

type errRow struct {
	err error
}

func (r errRow) Scan(_ ...any) error { return r.err }

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func withShortTimeout(t *testing.T) {
	t.Helper()
	prev := queueStateOpTimeout
	queueStateOpTimeout = 50 * time.Millisecond
	t.Cleanup(func() { queueStateOpTimeout = prev })
}

func runWithGuard(t *testing.T, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return; no per-call deadline was derived from the request context")
		return nil
	}
}

func TestUpsert_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	withShortTimeout(t)
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}}

	err := runWithGuard(t, func() error {
		return repo.Upsert(context.Background(), domain.EmptyQueueState(testUser()))
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestGetForUser_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	withShortTimeout(t)
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}}

	err := runWithGuard(t, func() error {
		_, err := repo.GetForUser(context.Background(), testUser())
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
