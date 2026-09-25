package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	return pgconn.NewCommandTag("INSERT 0 1"), nil
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
	if !errors.Is(err, ports.ErrCorruptStoredState) {
		t.Fatalf("corrupt stored row must satisfy ports.ErrCorruptStoredState so the service can degrade, got %v", err)
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

// recordingMetrics is a ports.QueueStateMetrics double that counts each
// degradation signal, so a test can assert a counter fired on a failure path.
type recordingMetrics struct {
	corruptStoredState   int
	queueStateOpTimedOut int
}

func (m *recordingMetrics) CorruptStoredState()   { m.corruptStoredState++ }
func (m *recordingMetrics) QueueStateOpTimedOut() { m.queueStateOpTimedOut++ }

// TestGetForUser_CorruptRow_IncrementsMetric reproduces the missing health
// signal: a corrupt stored row degrades to a server fault but, before this
// change, incremented no counter — only a log line.
func TestGetForUser_CorruptRow_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	repo := &PgxQueueStateRepository{pool: rowQuerier{row: corruptRow{repeatMode: "sideways"}}, metrics: m}

	if _, err := repo.GetForUser(context.Background(), testUser()); err == nil {
		t.Fatal("precondition: a corrupt stored row must still return an error")
	}
	if m.corruptStoredState != 1 {
		t.Fatalf("CorruptStoredState counter = %d, want 1 after a corrupt row", m.corruptStoredState)
	}
}

// TestGetForUser_HealthyRow_RecordsNoCorruption guards against counting a
// clean read as corruption.
func TestGetForUser_HealthyRow_RecordsNoCorruption(t *testing.T) {
	m := &recordingMetrics{}
	repo := &PgxQueueStateRepository{pool: newFakeStore(), metrics: m}
	user := testUser()

	if err := repo.Upsert(context.Background(), domain.EmptyQueueState(user)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := repo.GetForUser(context.Background(), user); err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if m.corruptStoredState != 0 {
		t.Fatalf("CorruptStoredState counter = %d, want 0 for a healthy row", m.corruptStoredState)
	}
}

// TestGetForUser_Timeout_IncrementsMetric reproduces the missing health signal
// for a DB op that blows its per-op deadline.
func TestGetForUser_Timeout_IncrementsMetric(t *testing.T) {
	withShortTimeout(t)
	m := &recordingMetrics{}
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: m}

	err := runWithGuard(t, func() error {
		_, err := repo.GetForUser(context.Background(), testUser())
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if m.queueStateOpTimedOut != 1 {
		t.Fatalf("QueueStateOpTimedOut counter = %d, want 1 after an op timeout", m.queueStateOpTimedOut)
	}
}

// TestDeleteForUser_ClientCancel_RecordsNoTimeout proves a caller-side cancel
// is not misattributed to the database being slow.
func TestDeleteForUser_ClientCancel_RecordsNoTimeout(t *testing.T) {
	m := &recordingMetrics{}
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: m}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client is already gone

	if err := repo.DeleteForUser(ctx, testUser()); err == nil {
		t.Fatal("precondition: a canceled context must surface an error")
	}
	if m.queueStateOpTimedOut != 0 {
		t.Fatalf("QueueStateOpTimedOut counter = %d, want 0 for a client cancel", m.queueStateOpTimedOut)
	}
}

func TestGetForUser_CorruptStoredRepeatMode_MapsToServerFault(t *testing.T) {
	repo := &PgxQueueStateRepository{pool: rowQuerier{row: corruptRow{repeatMode: "sideways"}}, metrics: ports.NoopQueueStateMetrics()}

	_, err := repo.GetForUser(context.Background(), testUser())
	assertServerFault(t, err)
}

func TestGetForUser_StoredQueueExceedsMax_MapsToServerFault(t *testing.T) {
	oversized := make([]string, domain.MaxQueueLength+1)
	repo := &PgxQueueStateRepository{pool: rowQuerier{row: corruptRow{repeatMode: "off", trackIds: oversized}}, metrics: ports.NoopQueueStateMetrics()}

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

func TestUpsert_RejectsInvariantViolatingLiteral(t *testing.T) {
	q := &capturingQuerier{}
	repo := &PgxQueueStateRepository{pool: q}

	// A bare struct literal bypasses NewQueueState entirely: CurrentIdx points
	// past the only track, an out-of-bounds index the constructor rejects. The
	// persistence boundary must re-validate so this cannot reach a stored row.
	invalid := &domain.QueueState{
		UserId:       testUser(),
		TrackIds:     []string{"only-track"},
		NaturalOrder: []string{"only-track"},
		CurrentIdx:   7,
		RepeatMode:   domain.RepeatOff,
		UpdatedAt:    time.Now().UTC(),
	}

	err := repo.Upsert(context.Background(), invalid)
	if err == nil {
		t.Fatal("Upsert accepted a QueueState whose CurrentIdx is out of range; a struct-literal bypass reached the database")
	}
	if q.sql != "" {
		t.Fatalf("Upsert issued SQL for an invalid state (%q); it must reject before writing", q.sql)
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

// argsQuerier records the SQL and bound arguments of the last Exec.
type argsQuerier struct {
	sql  string
	args []any
}

func (q *argsQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	q.sql, q.args = sql, args
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (*argsQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: nil}
}

// TestUpsert_OrdersSavesOnDatabaseClock pins the #1121 fix: the ordering value
// the stale guard compares is derived from the database clock inside the
// statement, never bound from the API process's own wall-clock reading, which
// clock steps and instance skew make untrustworthy.
func TestUpsert_OrdersSavesOnDatabaseClock(t *testing.T) {
	q := &argsQuerier{}
	repo := &PgxQueueStateRepository{pool: q}
	fastClock := domain.EmptyQueueState(testUser())
	fastClock.UpdatedAt = time.Now().UTC().Add(time.Hour)

	if err := repo.Upsert(context.Background(), fastClock); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for i, arg := range q.args {
		if _, ok := arg.(time.Time); ok {
			t.Fatalf("Upsert binds a process wall-clock time as argument $%d; saves must be ordered on the database clock", i+1)
		}
	}
	normalized := strings.Join(strings.Fields(q.sql), " ")
	if want := "clock_timestamp() - $9::bigint * interval '1 microsecond'"; !strings.Contains(normalized, want) {
		t.Fatalf("Upsert SQL does not derive updated_at from the database clock (%q).\nSQL: %s", want, normalized)
	}
}

func TestHandlingAge_MeasuredWhenEncodedNotWhenBound(t *testing.T) {
	age := handlingAge{stampedAt: time.Now()}
	time.Sleep(20 * time.Millisecond)

	v, err := age.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got := v.(int64); got < (20 * time.Millisecond).Microseconds() {
		t.Fatalf("age = %dµs, want >= 20000µs; time waiting before encoding (e.g. for a pooled connection) must count", got)
	}
}

func TestHandlingAge_FutureStampClampsToZero(t *testing.T) {
	v, err := handlingAge{stampedAt: time.Now().UTC().Add(time.Hour)}.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got := v.(int64); got != 0 {
		t.Fatalf("age = %dµs, want 0; a fast clock must not push the row ahead of the database clock", got)
	}
}

// tagQuerier returns a fixed command tag from Exec, standing in for Postgres'
// reply to the upsert: "INSERT 0 1" when the row was written, "INSERT 0 0" when
// the ON CONFLICT ... WHERE guard rejected the update.
type tagQuerier struct {
	tag string
}

func (q tagQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(q.tag), nil
}

func (tagQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: nil}
}

func TestUpsert_GuardRejectedWriteIsReportedNotSilent(t *testing.T) {
	repo := &PgxQueueStateRepository{pool: tagQuerier{tag: "INSERT 0 0"}}

	err := repo.Upsert(context.Background(), domain.EmptyQueueState(testUser()))

	if !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("a write the stale guard rejected (0 rows) must return ErrStaleQueueWrite, got %v", err)
	}
	var se httputil.StatusError
	if !errors.As(err, &se) || se.HTTPStatus() != http.StatusConflict {
		t.Fatalf("stale write must classify as a 409 StatusError, got %v", err)
	}
}

func TestUpsert_AppliedWriteReportsSuccess(t *testing.T) {
	repo := &PgxQueueStateRepository{pool: tagQuerier{tag: "INSERT 0 1"}}

	if err := repo.Upsert(context.Background(), domain.EmptyQueueState(testUser())); err != nil {
		t.Fatalf("a write that affected one row must succeed, got %v", err)
	}
}

// fakeStore is a stateful querier that emulates the playback_queue_state table
// against an in-memory map, honoring the save and erasure INSERT..ON CONFLICT
// shapes and the SELECT the adapter issues. It lets the erasure round-trip be
// exercised without a live database, dropping the row where Postgres blanks it
// and stamps erased_at, which is the same thing to every reader. What the
// stamp decides — which of two concurrent writers wins — only Postgres can
// answer, so that is held by the integration tests.
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
	normalized := strings.Join(strings.Fields(sql), " ")
	switch {
	case strings.Contains(normalized, "INSERT INTO playback_queue_state (user_id, updated_at, erased_at)"):
		delete(f.rows, id)
	case strings.HasPrefix(normalized, "INSERT INTO playback_queue_state (user_id, track_ids"):
		f.rows[id] = storedRow{
			trackIds:     args[1].([]string),
			currentIdx:   args[2].(int),
			positionMs:   args[3].(int64),
			shuffled:     args[4].(bool),
			repeatMode:   args[5].(string),
			sourceId:     args[6].(string),
			naturalOrder: args[7].([]string),
			updatedAt:    time.Now(),
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
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
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: ports.NoopQueueStateMetrics()}

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
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: ports.NoopQueueStateMetrics()}

	err := runWithGuard(t, func() error {
		return repo.Upsert(context.Background(), domain.EmptyQueueState(testUser()))
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// positionQuerier stands in for Postgres' reply to UpdatePosition's statement:
// (applied, matched). It records the SQL and bound arguments.
type positionQuerier struct {
	applied, matched bool
	sql              string
	args             []any
}

func (*positionQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("UpdatePosition must not use Exec")
}

func (q *positionQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.sql, q.args = sql, args
	return boolsRow{q.applied, q.matched}
}

type boolsRow [2]bool

func (r boolsRow) Scan(dest ...any) error {
	for i, d := range dest {
		p, ok := d.(*bool)
		if !ok || p == nil || i >= len(r) {
			return errors.New("boolsRow scans exactly two *bool destinations")
		}
		*p = r[i]
	}
	return nil
}

func testPosition(t *testing.T) *domain.QueuePosition {
	t.Helper()
	p, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId: testUser(), CurrentIdx: 3, CurrentTrackId: "t4", PositionMs: 61000,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	return p
}

// TestUpdatePosition_BindsNoTrackList pins the #1126 fix: the position-only
// save sends no track list to the database, so neither array is encoded,
// transferred or written, and it is still ordered on the database clock by the
// same stale guard as a full save.
func TestUpdatePosition_BindsNoTrackList(t *testing.T) {
	q := &positionQuerier{applied: true, matched: true}
	repo := &PgxQueueStateRepository{pool: q, metrics: ports.NoopQueueStateMetrics()}

	if err := repo.UpdatePosition(context.Background(), testPosition(t)); err != nil {
		t.Fatalf("UpdatePosition: %v", err)
	}

	for i, arg := range q.args {
		switch arg.(type) {
		case []string:
			t.Fatalf("UpdatePosition binds a track list as argument $%d; a position-only save must not send the queue", i+1)
		case time.Time:
			t.Fatalf("UpdatePosition binds a process wall-clock time as argument $%d; saves must be ordered on the database clock", i+1)
		}
	}
	normalized := strings.Join(strings.Fields(q.sql), " ")
	for _, want := range []string{
		"statement_timestamp() - $4::bigint * interval '1 microsecond'",
		"q.updated_at <= handled.at",
		"q.track_ids[$2::int + 1] = $5",
	} {
		if !strings.Contains(normalized, want) {
			t.Errorf("UpdatePosition SQL lacks %q.\nSQL: %s", want, normalized)
		}
	}
	if strings.Contains(normalized, "SET track_ids") || strings.Contains(normalized, "natural_order =") {
		t.Errorf("UpdatePosition SQL writes a track list.\nSQL: %s", normalized)
	}
}

func TestUpdatePosition_ClassifiesUnappliedSaves(t *testing.T) {
	tests := []struct {
		name             string
		applied, matched bool
		want             error
	}{
		{name: "applied", applied: true, matched: true, want: nil},
		{name: "guard rejected a stale save", matched: true, want: domain.ErrStaleQueueWrite},
		{name: "no stored queue holds the track at the index", want: domain.ErrQueuePositionMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &PgxQueueStateRepository{pool: &positionQuerier{applied: tt.applied, matched: tt.matched}, metrics: ports.NoopQueueStateMetrics()}

			err := repo.UpdatePosition(context.Background(), testPosition(t))

			if tt.want == nil {
				if err != nil {
					t.Fatalf("UpdatePosition = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("UpdatePosition = %v, want %v", err, tt.want)
			}
			var se httputil.StatusError
			if !errors.As(err, &se) || se.HTTPStatus() != http.StatusConflict {
				t.Fatalf("an unapplied position save must classify as a 409, got %v", err)
			}
		})
	}
}

func TestUpdatePosition_RejectsInvariantViolatingLiteral(t *testing.T) {
	q := &positionQuerier{applied: true, matched: true}
	repo := &PgxQueueStateRepository{pool: q, metrics: ports.NoopQueueStateMetrics()}

	err := repo.UpdatePosition(context.Background(), &domain.QueuePosition{UserId: testUser(), CurrentIdx: -1, CurrentTrackId: "t1"})

	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("UpdatePosition accepted a negative CurrentIdx literal, got %v", err)
	}
	if q.sql != "" {
		t.Fatalf("UpdatePosition issued SQL for an invalid position: %q", q.sql)
	}
}

func TestUpdatePosition_Timeout_IncrementsMetric(t *testing.T) {
	withShortTimeout(t)
	m := &recordingMetrics{}
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: m}

	err := runWithGuard(t, func() error {
		return repo.UpdatePosition(context.Background(), testPosition(t))
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if m.queueStateOpTimedOut != 1 {
		t.Fatalf("QueueStateOpTimedOut counter = %d, want 1", m.queueStateOpTimedOut)
	}
}

func TestGetForUser_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	withShortTimeout(t)
	repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: ports.NoopQueueStateMetrics()}

	err := runWithGuard(t, func() error {
		_, err := repo.GetForUser(context.Background(), testUser())
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// captureLogs redirects the default slog logger to a buffer for the duration of
// the test, so a test can assert which fields a structured log line carries.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// faultRecord decodes the single captured log line carrying msg, so a test
// asserts on fields rather than on substrings of a log blob.
func faultRecord(t *testing.T, logs *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec["msg"] != msg {
			continue
		}
		if found != nil {
			t.Fatalf("%q emitted more than once; logs=%s", msg, logs.String())
		}
		found = rec
	}
	if found == nil {
		t.Fatalf("no %q log record; logs=%s", msg, logs.String())
	}
	return found
}

// faultingQuerier fails every op with one error, standing in for a database that
// refuses the connection, exhausts its pool or rejects the statement.
type faultingQuerier struct {
	cause error
}

func (q faultingQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, q.cause
}

func (q faultingQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: q.cause}
}

func userPosition(t *testing.T, userId shared.UserId) *domain.QueuePosition {
	t.Helper()
	p, err := domain.NewQueuePosition(domain.QueuePositionInput{
		UserId: userId, CurrentIdx: 3, CurrentTrackId: "t4", PositionMs: 61000,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	return p
}

// queueStateOps is every persistence op reachable from a request, each named as
// the fault line names it.
var queueStateOps = []struct {
	op   string
	call func(*testing.T, *PgxQueueStateRepository, shared.UserId) error
}{
	{op: "upsert", call: func(_ *testing.T, repo *PgxQueueStateRepository, user shared.UserId) error {
		return repo.Upsert(context.Background(), domain.EmptyQueueState(user))
	}},
	{op: "update_position", call: func(t *testing.T, repo *PgxQueueStateRepository, user shared.UserId) error {
		return repo.UpdatePosition(context.Background(), userPosition(t, user))
	}},
	{op: "get_for_user", call: func(_ *testing.T, repo *PgxQueueStateRepository, user shared.UserId) error {
		_, err := repo.GetForUser(context.Background(), user)
		return err
	}},
	{op: "delete_for_user", call: func(_ *testing.T, repo *PgxQueueStateRepository, user shared.UserId) error {
		return repo.DeleteForUser(context.Background(), user)
	}},
}

// TestQueueStateOp_UnclassifiedFailure_NamesTheAffectedUser pins #1595: a
// database fault surfaces as a bare 500 whose only log line carries method and
// path, so without this one an operator cannot tell whose write was lost.
func TestQueueStateOp_UnclassifiedFailure_NamesTheAffectedUser(t *testing.T) {
	for _, tt := range queueStateOps {
		t.Run(tt.op, func(t *testing.T) {
			logs := captureLogs(t)
			dbDown := errors.New("connection refused")
			repo := &PgxQueueStateRepository{pool: faultingQuerier{cause: dbDown}, metrics: ports.NoopQueueStateMetrics()}
			user := testUser()

			if err := tt.call(t, repo, user); !errors.Is(err, dbDown) {
				t.Fatalf("precondition: the failure must still propagate, got %v", err)
			}

			rec := faultRecord(t, logs, "playback.queue_state_op_failed")
			if rec["user_id"] != user.String() {
				t.Errorf("user_id = %v, want %q; the fault names no user to act on", rec["user_id"], user.String())
			}
			if rec["op"] != tt.op {
				t.Errorf("op = %v, want %q", rec["op"], tt.op)
			}
			if got := fmt.Sprint(rec["error"]); !strings.Contains(got, "connection refused") {
				t.Errorf("error = %q, want the cause of the failure", got)
			}
		})
	}
}

// TestQueueStateOp_ClassifiedOutcome_EmitsNoFaultLine keeps the fault line to
// the failures nothing else reports: a blown deadline is already counted, a
// cancel is the client leaving, and no stored row is how an empty queue reads.
func TestQueueStateOp_ClassifiedOutcome_EmitsNoFaultLine(t *testing.T) {
	tests := []struct {
		outcome string
		act     func(*testing.T)
	}{
		{outcome: "op timeout", act: func(t *testing.T) {
			withShortTimeout(t)
			repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: ports.NoopQueueStateMetrics()}
			err := runWithGuard(t, func() error {
				return repo.Upsert(context.Background(), domain.EmptyQueueState(testUser()))
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("precondition: err = %v, want context.DeadlineExceeded", err)
			}
		}},
		{outcome: "client cancel", act: func(t *testing.T) {
			repo := &PgxQueueStateRepository{pool: blockingQuerier{}, metrics: ports.NoopQueueStateMetrics()}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := repo.DeleteForUser(ctx, testUser()); !errors.Is(err, context.Canceled) {
				t.Fatalf("precondition: err = %v, want context.Canceled", err)
			}
		}},
		{outcome: "no stored queue", act: func(t *testing.T) {
			repo := &PgxQueueStateRepository{pool: newFakeStore(), metrics: ports.NoopQueueStateMetrics()}
			state, err := repo.GetForUser(context.Background(), testUser())
			if state != nil || err != nil {
				t.Fatalf("precondition: an absent queue reads as (nil, nil), got (%v, %v)", state, err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.outcome, func(t *testing.T) {
			logs := captureLogs(t)

			tt.act(t)

			if strings.Contains(logs.String(), "playback.queue_state_op_failed") {
				t.Fatalf("%s was reported as an unclassified database fault; logs=%s", tt.outcome, logs.String())
			}
		})
	}
}

// TestUpsert_FailureLogNamesTheUserNotTheQueue holds the module's log hygiene:
// the stored queue is the PII an erasure exists to remove (#1097), so a fault
// line names its owner and never its contents.
func TestUpsert_FailureLogNamesTheUserNotTheQueue(t *testing.T) {
	logs := captureLogs(t)
	repo := &PgxQueueStateRepository{pool: faultingQuerier{cause: errors.New("connection refused")}, metrics: ports.NoopQueueStateMetrics()}
	const privateSourceId = "search:mac demarco"
	const privateTrackId = "3b9c77e4-private-track-id"
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", privateTrackId},
		CurrentIdx: 1,
		RepeatMode: domain.RepeatOff,
		SourceId:   privateSourceId,
	})
	if err != nil {
		t.Fatalf("build state: %v", err)
	}

	if err := repo.Upsert(context.Background(), state); err == nil {
		t.Fatal("precondition: a refused connection must surface an error")
	}

	out := logs.String()
	for _, leaked := range []string{privateTrackId, privateSourceId, "track_id", "source_id"} {
		if strings.Contains(out, leaked) {
			t.Errorf("the fault line carries %q; it must name the queue's owner, not its contents.\nlogs=%s", leaked, out)
		}
	}
}

type erroringQuerier struct {
	err error
}

func (f erroringQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f erroringQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow(f)
}

func failingRepo(err error) *PgxQueueStateRepository {
	return &PgxQueueStateRepository{pool: erroringQuerier{err: err}, metrics: ports.NoopQueueStateMetrics()}
}

func TestDeleteForUser_DeadlineExceeded_IsRetryable503(t *testing.T) {
	err := failingRepo(context.DeadlineExceeded).DeleteForUser(context.Background(), testUser())

	if !errors.Is(err, ports.ErrQueueStateUnavailable) {
		t.Fatalf("err = %v, want ErrQueueStateUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("cause lost from the chain: %v", err)
	}
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodDelete, "/queue-state", nil), err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if !strings.Contains(rec.Body.String(), `"code":"playback.unavailable"`) {
		t.Errorf("body = %q, want code playback.unavailable", rec.Body.String())
	}
}

func TestQueueStateOp_TransientClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"connection refused", &pgconn.ConnectError{}, true},
		{"admin shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"constraint violation", &pgconn.PgError{Code: "23505"}, false},
		{"client cancel", context.Canceled, false},
		{"no rows", pgx.ErrNoRows, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := failingRepo(tc.err).DeleteForUser(context.Background(), testUser())
			if got := errors.Is(err, ports.ErrQueueStateUnavailable); got != tc.want {
				t.Fatalf("unavailable = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestGetForUser_TransientFailureIsNotCorruptState(t *testing.T) {
	_, err := failingRepo(context.DeadlineExceeded).GetForUser(context.Background(), testUser())

	if errors.Is(err, ports.ErrCorruptStoredState) || !errors.Is(err, ports.ErrQueueStateUnavailable) {
		t.Fatalf("err = %v, want unavailable and not corrupt", err)
	}
}
