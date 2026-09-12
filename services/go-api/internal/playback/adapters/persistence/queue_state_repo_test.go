package persistence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
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

type capturingQuerier struct {
	sql string
}

func (c *capturingQuerier) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.sql = sql
	return pgconn.CommandTag{}, nil
}

func (c *capturingQuerier) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	c.sql = sql
	return errRow{err: nil}
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

type corruptRow struct {
	trackIds   []string
	repeatMode string
}

func (r corruptRow) Scan(dest ...any) error {
	*(dest[0].(*[]string)) = r.trackIds
	*(dest[4].(*string)) = r.repeatMode
	return nil
}

type rowQuerier struct {
	row pgx.Row
}

func (rowQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (q rowQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return q.row
}

func assertServerFault(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error for a corrupt stored row, got nil")
	}
	var se httputil.StatusError
	if errors.As(err, &se) {
		t.Fatalf("corrupt stored state surfaced as HTTP %d (%v); a server-side data fault must map to 500, not a client error", se.HTTPStatus(), err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	httputil.HandleServiceError(rec, req, err)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("HandleServiceError wrote %d for a corrupt stored row, want 500", rec.Code)
	}
}

func TestGetForUser_CorruptStoredRepeatMode_MapsToServerFault(t *testing.T) {
	repo := &PgxQueueStateRepository{pool: rowQuerier{row: corruptRow{repeatMode: "sideways"}}}

	_, err := repo.GetForUser(context.Background(), testUser())
	assertServerFault(t, err)
}

func TestGetForUser_StoredQueueExceedsMax_MapsToServerFault(t *testing.T) {
	oversized := make([]string, domain.MaxQueueLength+1)
	repo := &PgxQueueStateRepository{pool: rowQuerier{row: corruptRow{repeatMode: "off", trackIds: oversized}}}

	_, err := repo.GetForUser(context.Background(), testUser())
	assertServerFault(t, err)
}

func TestSavePathValidationStaysClientFault(t *testing.T) {
	_, err := domain.NewQueueState(domain.QueueStateInput{PositionMs: -1})

	var se httputil.StatusError
	if !errors.As(err, &se) || se.HTTPStatus() != http.StatusBadRequest {
		t.Fatalf("client-input validation must stay a 400 StatusError, got %v", err)
	}
}

func TestUpsert_GuardsAgainstStaleClobber(t *testing.T) {
	q := &capturingQuerier{}
	repo := &PgxQueueStateRepository{pool: q}

	if err := repo.Upsert(context.Background(), domain.EmptyQueueState(testUser())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	normalized := strings.Join(strings.Fields(q.sql), " ")
	want := "WHERE playback_queue_state.updated_at <= EXCLUDED.updated_at"
	if !strings.Contains(normalized, want) {
		t.Fatalf("Upsert SQL lacks the ordering guard %q; an older snapshot can still clobber a newer one.\nSQL: %s", want, normalized)
	}
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
