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

// fakeStore is a stateful querier that emulates the playback_queue_state table
// against an in-memory map, honoring the INSERT..ON CONFLICT, SELECT, and
// DELETE shapes the adapter issues. It lets the delete round-trip be exercised
// without a live database.
type storedRow struct {
	trackIds     []string
	currentIdx   int
	positionMs   int64
	shuffled     bool
	repeatMode   string
	sourceId     string
	naturalOrder []string
	updatedAt    time.Time
}

type fakeStore struct {
	rows map[uuid.UUID]storedRow
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[uuid.UUID]storedRow{}}
}

func (f *fakeStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	id := args[0].(uuid.UUID)
	switch {
	case strings.HasPrefix(strings.TrimSpace(sql), "DELETE"):
		delete(f.rows, id)
	case strings.HasPrefix(strings.TrimSpace(sql), "INSERT"):
		f.rows[id] = storedRow{
			trackIds:     args[1].([]string),
			currentIdx:   args[2].(int),
			positionMs:   args[3].(int64),
			shuffled:     args[4].(bool),
			repeatMode:   args[5].(string),
			sourceId:     args[6].(string),
			naturalOrder: args[7].([]string),
			updatedAt:    args[8].(time.Time),
		}
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeStore) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	id := args[0].(uuid.UUID)
	row, ok := f.rows[id]
	return storeRow{row: row, ok: ok}
}

type storeRow struct {
	row storedRow
	ok  bool
}

func (r storeRow) Scan(dest ...any) error {
	if !r.ok {
		return pgx.ErrNoRows
	}
	*(dest[0].(*[]string)) = r.row.trackIds
	*(dest[1].(*int)) = r.row.currentIdx
	*(dest[2].(*int64)) = r.row.positionMs
	*(dest[3].(*bool)) = r.row.shuffled
	*(dest[4].(*string)) = r.row.repeatMode
	*(dest[5].(*string)) = r.row.sourceId
	*(dest[6].(*[]string)) = r.row.naturalOrder
	*(dest[7].(*time.Time)) = r.row.updatedAt
	return nil
}

func TestDeleteForUser_ErasesStoredState(t *testing.T) {
	repo := &PgxQueueStateRepository{pool: newFakeStore()}
	ctx := context.Background()
	user := testUser()

	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:     user,
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 1,
		RepeatMode: domain.RepeatOff,
		SourceId:   "search:mac demarco", // free-text PII lives in source_id
	})
	if err != nil {
		t.Fatalf("build state: %v", err)
	}
	if err := repo.Upsert(ctx, state); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err := repo.GetForUser(ctx, user); err != nil || got == nil {
		t.Fatalf("precondition: state must be readable before deletion (got=%v err=%v)", got, err)
	}

	if err := repo.DeleteForUser(ctx, user); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	got, err := repo.GetForUser(ctx, user)
	if err != nil {
		t.Fatalf("GetForUser after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("persisted queue state survived DeleteForUser: %+v", got)
	}
}

func TestDeleteForUser_IsScopedToUser(t *testing.T) {
	store := newFakeStore()
	repo := &PgxQueueStateRepository{pool: store}
	ctx := context.Background()
	target, other := testUser(), testUser()

	for _, u := range []shared.UserId{target, other} {
		state, _ := domain.NewQueueState(domain.QueueStateInput{
			UserId: u, RepeatMode: domain.RepeatOff,
		})
		if err := repo.Upsert(ctx, state); err != nil {
			t.Fatalf("Upsert(%v): %v", u.UUID(), err)
		}
	}

	if err := repo.DeleteForUser(ctx, target); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	if got, _ := repo.GetForUser(ctx, other); got == nil {
		t.Fatal("DeleteForUser erased another user's queue state; delete must be scoped to user_id")
	}
}

func TestDeleteForUser_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	withShortTimeout(t)
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}}

	err := runWithGuard(t, func() error {
		return repo.DeleteForUser(context.Background(), testUser())
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
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
