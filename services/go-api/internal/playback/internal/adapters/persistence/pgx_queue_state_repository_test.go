package persistence_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type timingOutQuerier struct{}

var errNoPerCallDeadline = errors.New("the call carried no per-call deadline within its timeout")

func timedOut(ctx context.Context, within time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > within {
		return errNoPerCallDeadline
	}
	return context.DeadlineExceeded
}

func (timingOutQuerier) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, timedOut(ctx, defaultQueueStateOpTimeout)
}

func (timingOutQuerier) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: timedOut(ctx, defaultQueueStateOpTimeout)}
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

const (
	maxQueueLength              = 10000
	erasureFenceWindow          = 15 * time.Minute
	defaultQueueStateOpTimeout  = 3 * time.Second
	defaultOwnerScanPageTimeout = 5 * time.Second
)

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func emptyQueueState(user shared.UserId) *playback.QueueState {
	state, err := playback.NewQueueState(playback.QueueStateInput{UserId: user})
	if err != nil {
		panic("an empty queue must always be valid: " + err.Error())
	}
	return state
}

func newQueuePosition(in playback.QueuePosition) (*playback.QueuePosition, error) {
	in.UpdatedAt = time.Now()
	if err := in.Validate(); err != nil {
		return nil, err
	}
	return &in, nil
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

func answeredStatus(err error) int {
	rec := httptest.NewRecorder()
	httputil.HandleServiceError(rec, httptest.NewRequest(http.MethodPut, "/queue-state", nil), err)
	return rec.Code
}

func assertServerFault(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error for a corrupt stored row, got nil")
	}
	if !errors.Is(err, playback.ErrCorruptStoredState) {
		t.Fatalf("corrupt stored row must satisfy playback.ErrCorruptStoredState so the service can degrade, got %v", err)
	}
	if got := answeredStatus(err); got != http.StatusInternalServerError {
		t.Fatalf("a corrupt stored row answered %d, want 500: a server-side data fault is not a client error", got)
	}
}

type recordingMetrics struct {
	corruptStoredState   int
	queueStateOpTimedOut int
	transientFailures    int
	staleWrites          int
}

func (m *recordingMetrics) CorruptStoredState()   { m.corruptStoredState++ }
func (m *recordingMetrics) QueueStateOpTimedOut() { m.queueStateOpTimedOut++ }

func (m *recordingMetrics) QueueStateOpFailedTransient() { m.transientFailures++ }
func (m *recordingMetrics) QueueStateStaleWrite()        { m.staleWrites++ }

func TestGetForUser_CorruptRow_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(rowQuerier{row: corruptRow{repeatMode: "sideways"}}, playback.WithQueueStateMetrics(m))

	if _, err := repo.GetForUser(context.Background(), testUser()); err == nil {
		t.Fatal("precondition: a corrupt stored row must still return an error")
	}
	if m.corruptStoredState != 1 {
		t.Fatalf("CorruptStoredState counter = %d, want 1 after a corrupt row", m.corruptStoredState)
	}
}

func TestGetForUser_HealthyRow_RecordsNoCorruption(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(newFakeStore(), playback.WithQueueStateMetrics(m))
	user := testUser()

	if err := repo.Upsert(context.Background(), emptyQueueState(user)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := repo.GetForUser(context.Background(), user); err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if m.corruptStoredState != 0 {
		t.Fatalf("CorruptStoredState counter = %d, want 0 for a healthy row", m.corruptStoredState)
	}
}

func TestGetForUser_Timeout_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{}, playback.WithQueueStateMetrics(m))

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

func TestDeleteForUser_ClientCancel_RecordsNoTimeout(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{}, playback.WithQueueStateMetrics(m))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := repo.DeleteForUser(ctx, testUser()); err == nil {
		t.Fatal("precondition: a canceled context must surface an error")
	}
	if m.queueStateOpTimedOut != 0 {
		t.Fatalf("QueueStateOpTimedOut counter = %d, want 0 for a client cancel", m.queueStateOpTimedOut)
	}
}

func TestGetForUser_CorruptStoredRepeatMode_MapsToServerFault(t *testing.T) {
	repo := playback.NewPgxQueueStateRepository(rowQuerier{row: corruptRow{repeatMode: "sideways"}})

	_, err := repo.GetForUser(context.Background(), testUser())
	assertServerFault(t, err)
}

func TestGetForUser_StoredQueueExceedsMax_MapsToServerFault(t *testing.T) {
	oversized := make([]string, maxQueueLength+1)
	repo := playback.NewPgxQueueStateRepository(rowQuerier{row: corruptRow{repeatMode: "off", trackIds: oversized}})

	_, err := repo.GetForUser(context.Background(), testUser())
	assertServerFault(t, err)
}

func TestSavePathValidationStaysClientFault(t *testing.T) {
	_, err := playback.NewQueueState(playback.QueueStateInput{PositionMs: -1})

	if got := answeredStatus(err); got != http.StatusBadRequest {
		t.Fatalf("client-input validation answered %d, want 400 (%v)", got, err)
	}
}

func TestUpsert_RejectsInvariantViolatingLiteral(t *testing.T) {
	q := &capturingQuerier{}
	repo := playback.NewPgxQueueStateRepository(q)

	invalid := &playback.QueueState{
		UserId:       testUser(),
		TrackIds:     []string{"only-track"},
		NaturalOrder: []string{"only-track"},
		CurrentIdx:   7,
		RepeatMode:   playback.RepeatOff,
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
	repo := playback.NewPgxQueueStateRepository(q)

	if err := repo.Upsert(context.Background(), emptyQueueState(testUser())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	normalized := strings.Join(strings.Fields(q.sql), " ")
	want := "WHERE playback_queue_state.updated_at <= EXCLUDED.updated_at"
	if !strings.Contains(normalized, want) {
		t.Fatalf("Upsert SQL lacks the ordering guard %q; an older snapshot can still clobber a newer one.\nSQL: %s", want, normalized)
	}
}

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

func TestUpsert_OrdersSavesOnDatabaseClock(t *testing.T) {
	q := &argsQuerier{}
	repo := playback.NewPgxQueueStateRepository(q)
	fastClock := emptyQueueState(testUser())
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

func boundHandlingAge(t *testing.T, state *playback.QueueState) driver.Valuer {
	t.Helper()
	q := &argsQuerier{}
	if err := playback.NewPgxQueueStateRepository(q).Upsert(context.Background(), state); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	age, ok := q.args[8].(driver.Valuer)
	if !ok {
		t.Fatalf("Upsert binds $9 as %T, want a value encoded when the statement is sent", q.args[8])
	}
	return age
}

func TestHandlingAge_MeasuredWhenEncodedNotWhenBound(t *testing.T) {
	age := boundHandlingAge(t, emptyQueueState(testUser()))
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
	fastClock := emptyQueueState(testUser())
	fastClock.UpdatedAt = time.Now().UTC().Add(time.Hour)

	v, err := boundHandlingAge(t, fastClock).Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got := v.(int64); got != 0 {
		t.Fatalf("age = %dµs, want 0; a fast clock must not push the row ahead of the database clock", got)
	}
}

func boundSaveGrace(t *testing.T, opts ...func(*playback.PgxQueueStateRepository)) any {
	t.Helper()
	q := &argsQuerier{}
	if err := playback.NewPgxQueueStateRepository(q, opts...).Upsert(context.Background(), emptyQueueState(testUser())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return q.args[9]
}

func TestUpsert_ErasureSaveGrace(t *testing.T) {
	for name, tt := range map[string]struct {
		opts []func(*playback.PgxQueueStateRepository)
		want time.Duration
	}{
		"defaults to the op timeout": {want: defaultQueueStateOpTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			if got := boundSaveGrace(t, tt.opts...); got != tt.want.Microseconds() {
				t.Errorf("Upsert binds a save grace of %vµs as $10, want %dµs", got, tt.want.Microseconds())
			}
		})
	}
}

type deadlineQuerier struct {
	left time.Duration
}

func (q *deadlineQuerier) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		q.left = time.Until(deadline)
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (q *deadlineQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	_, _ = q.Exec(ctx, sql, args...)
	return errRow{err: pgx.ErrNoRows}
}

func TestQueueStateOp_Deadline(t *testing.T) {
	for name, tt := range map[string]struct {
		opts []func(*playback.PgxQueueStateRepository)
		want time.Duration
	}{
		"defaults to the request timeout": {want: defaultQueueStateOpTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			q := &deadlineQuerier{}
			if err := playback.NewPgxQueueStateRepository(q, tt.opts...).DeleteForUser(context.Background(), testUser()); err != nil {
				t.Fatalf("DeleteForUser: %v", err)
			}
			if q.left > tt.want || q.left < tt.want-time.Second {
				t.Errorf("the database call had %s left, want its deadline %s away", q.left, tt.want)
			}
		})
	}
}

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
	repo := playback.NewPgxQueueStateRepository(tagQuerier{tag: "INSERT 0 0"})

	err := repo.Upsert(context.Background(), emptyQueueState(testUser()))

	if !errors.Is(err, playback.ErrStaleQueueWrite) {
		t.Fatalf("a write the stale guard rejected (0 rows) must return ErrStaleQueueWrite, got %v", err)
	}
	if got := answeredStatus(err); got != http.StatusConflict {
		t.Fatalf("a stale write answered %d, want 409 (%v)", got, err)
	}
}

func TestUpsert_AppliedWriteReportsSuccess(t *testing.T) {
	repo := playback.NewPgxQueueStateRepository(tagQuerier{tag: "INSERT 0 1"})

	if err := repo.Upsert(context.Background(), emptyQueueState(testUser())); err != nil {
		t.Fatalf("a write that affected one row must succeed, got %v", err)
	}
}

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
	repo := playback.NewPgxQueueStateRepository(newFakeStore())
	ctx := context.Background()
	user := testUser()

	state, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:     user,
		TrackIds:   []string{trk("a"), trk("b")},
		CurrentIdx: 1,
		RepeatMode: playback.RepeatOff,
		SourceId:   "search:mac demarco",
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
	repo := playback.NewPgxQueueStateRepository(store)
	ctx := context.Background()
	target, other := testUser(), testUser()

	for _, u := range []shared.UserId{target, other} {
		state, _ := playback.NewQueueState(playback.QueueStateInput{
			UserId: u, RepeatMode: playback.RepeatOff,
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
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{})

	err := runWithGuard(t, func() error {
		return repo.DeleteForUser(context.Background(), testUser())
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
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
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{})

	err := runWithGuard(t, func() error {
		return repo.Upsert(context.Background(), emptyQueueState(testUser()))
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

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

func testPosition(t *testing.T) *playback.QueuePosition {
	t.Helper()
	p, err := newQueuePosition(playback.QueuePosition{
		UserId: testUser(), CurrentIdx: 3, CurrentTrackId: trk("t4"), PositionMs: 61000,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	return p
}

func TestUpdatePosition_BindsNoTrackList(t *testing.T) {
	q := &positionQuerier{applied: true, matched: true}
	repo := playback.NewPgxQueueStateRepository(q)

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
		"regexp_replace(lower(q.track_ids[$2::int + 1]), '^urn:uuid:|[{}-]', '', 'g') = replace($5::text, '-', '')",
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
		{name: "guard rejected a stale save", matched: true, want: playback.ErrStaleQueueWrite},
		{name: "no stored queue holds the track at the index", want: playback.ErrQueuePositionMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := playback.NewPgxQueueStateRepository(&positionQuerier{applied: tt.applied, matched: tt.matched})

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
			if got := answeredStatus(err); got != http.StatusConflict {
				t.Fatalf("an unapplied position save answered %d, want 409 (%v)", got, err)
			}
		})
	}
}

func TestUpdatePosition_RejectsInvariantViolatingLiteral(t *testing.T) {
	q := &positionQuerier{applied: true, matched: true}
	repo := playback.NewPgxQueueStateRepository(q)

	err := repo.UpdatePosition(context.Background(), &playback.QueuePosition{UserId: testUser(), CurrentIdx: -1, CurrentTrackId: "t1"})

	if got := answeredStatus(err); got != http.StatusBadRequest {
		t.Fatalf("UpdatePosition with a negative CurrentIdx literal answered %d, want 400 (%v)", got, err)
	}
	if q.sql != "" {
		t.Fatalf("UpdatePosition issued SQL for an invalid position: %q", q.sql)
	}
}

func TestUpdatePosition_Timeout_IncrementsMetric(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{}, playback.WithQueueStateMetrics(m))

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
	repo := playback.NewPgxQueueStateRepository(timingOutQuerier{})

	err := runWithGuard(t, func() error {
		_, err := repo.GetForUser(context.Background(), testUser())
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

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

type faultingQuerier struct {
	cause error
}

func (q faultingQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, q.cause
}

func (q faultingQuerier) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: q.cause}
}

func userPosition(t *testing.T, userId shared.UserId) *playback.QueuePosition {
	t.Helper()
	p, err := newQueuePosition(playback.QueuePosition{
		UserId: userId, CurrentIdx: 3, CurrentTrackId: trk("t4"), PositionMs: 61000,
	})
	if err != nil {
		t.Fatalf("NewQueuePosition: %v", err)
	}
	return p
}

var queueStateOps = []struct {
	op   string
	call func(*testing.T, playback.QueueStateRepository, shared.UserId) error
}{
	{op: "upsert", call: func(_ *testing.T, repo playback.QueueStateRepository, user shared.UserId) error {
		return repo.Upsert(context.Background(), emptyQueueState(user))
	}},
	{op: "update_position", call: func(t *testing.T, repo playback.QueueStateRepository, user shared.UserId) error {
		return repo.UpdatePosition(context.Background(), userPosition(t, user))
	}},
	{op: "get_for_user", call: func(_ *testing.T, repo playback.QueueStateRepository, user shared.UserId) error {
		_, err := repo.GetForUser(context.Background(), user)
		return err
	}},
	{op: "delete_for_user", call: func(_ *testing.T, repo playback.QueueStateRepository, user shared.UserId) error {
		return repo.DeleteForUser(context.Background(), user)
	}},
}

func TestQueueStateOp_UnclassifiedFailure_NamesTheAffectedUser(t *testing.T) {
	for _, tt := range queueStateOps {
		t.Run(tt.op, func(t *testing.T) {
			logs := captureLogs(t)
			dbDown := errors.New("connection refused")
			repo := playback.NewPgxQueueStateRepository(faultingQuerier{cause: dbDown})
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

func TestQueueStateOp_ClassifiedOutcome_EmitsNoFaultLine(t *testing.T) {
	tests := []struct {
		outcome string
		act     func(*testing.T)
	}{
		{outcome: "op timeout", act: func(t *testing.T) {
			repo := playback.NewPgxQueueStateRepository(timingOutQuerier{})
			err := runWithGuard(t, func() error {
				return repo.Upsert(context.Background(), emptyQueueState(testUser()))
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("precondition: err = %v, want context.DeadlineExceeded", err)
			}
		}},
		{outcome: "client cancel", act: func(t *testing.T) {
			repo := playback.NewPgxQueueStateRepository(timingOutQuerier{})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := repo.DeleteForUser(ctx, testUser()); !errors.Is(err, context.Canceled) {
				t.Fatalf("precondition: err = %v, want context.Canceled", err)
			}
		}},
		{outcome: "no stored queue", act: func(t *testing.T) {
			repo := playback.NewPgxQueueStateRepository(newFakeStore())
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

func TestUpsert_FailureLogNamesTheUserNotTheQueue(t *testing.T) {
	logs := captureLogs(t)
	repo := playback.NewPgxQueueStateRepository(faultingQuerier{cause: errors.New("connection refused")})
	const privateSourceId = "search:mac demarco"
	const privateTrackId = "3b9c77e4-5d1a-4f0e-9c2b-7a6e1f4d8c30"
	state, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{trk("a"), privateTrackId},
		CurrentIdx: 1,
		RepeatMode: playback.RepeatOff,
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

func failingRepo(err error) playback.QueueStateRepository {
	return playback.NewPgxQueueStateRepository(erroringQuerier{err: err})
}

func TestDeleteForUser_DeadlineExceeded_IsRetryable503(t *testing.T) {
	err := failingRepo(context.DeadlineExceeded).DeleteForUser(context.Background(), testUser())

	if !errors.Is(err, playback.ErrQueueStateUnavailable) {
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
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"connection dropped without a message", fmt.Errorf("read: %w", io.EOF), true},
		{"statement on a closed connection", pgconn.ErrConnClosed, true},
		{"admin shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"constraint violation", &pgconn.PgError{Code: "23505"}, false},
		{"client cancel", context.Canceled, false},
		{"no rows", pgx.ErrNoRows, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := failingRepo(tc.err).DeleteForUser(context.Background(), testUser())
			if got := errors.Is(err, playback.ErrQueueStateUnavailable); got != tc.want {
				t.Fatalf("unavailable = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

func TestGetForUser_TransientFailureIsNotCorruptState(t *testing.T) {
	_, err := failingRepo(context.DeadlineExceeded).GetForUser(context.Background(), testUser())

	if errors.Is(err, playback.ErrCorruptStoredState) || !errors.Is(err, playback.ErrQueueStateUnavailable) {
		t.Fatalf("err = %v, want unavailable and not corrupt", err)
	}
}

type failingStore struct{ err error }

func (f failingStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f failingStore) QueryRow(context.Context, string, ...any) pgx.Row { return errRow(f) }

func TestRunOp_TransientFault_CountsTransientNotTimeout(t *testing.T) {
	m := &recordingMetrics{}
	repo := playback.NewPgxQueueStateRepository(failingStore{err: &pgconn.PgError{Code: "57P01"}}, playback.WithQueueStateMetrics(m))

	if err := repo.Upsert(context.Background(), emptyQueueState(testUser())); !errors.Is(err, playback.ErrQueueStateUnavailable) {
		t.Fatalf("Upsert = %v, want ErrQueueStateUnavailable", err)
	}
	if _, err := repo.GetForUser(context.Background(), testUser()); !errors.Is(err, playback.ErrQueueStateUnavailable) {
		t.Fatalf("GetForUser = %v, want ErrQueueStateUnavailable", err)
	}
	if m.transientFailures != 2 || m.queueStateOpTimedOut != 0 {
		t.Fatalf("transient = %d, timeouts = %d; want 2 and 0", m.transientFailures, m.queueStateOpTimedOut)
	}
}

func TestRunOp_ClientCancelAndNoRows_MoveNoFailureCounter(t *testing.T) {
	for name, cause := range map[string]error{"client cancel": context.Canceled, "no rows": pgx.ErrNoRows} {
		t.Run(name, func(t *testing.T) {
			m := &recordingMetrics{}
			repo := playback.NewPgxQueueStateRepository(failingStore{err: cause}, playback.WithQueueStateMetrics(m))

			_, _ = repo.GetForUser(context.Background(), testUser())

			if m.transientFailures != 0 || m.staleWrites != 0 {
				t.Fatalf("transient = %d, stale = %d; want 0 and 0", m.transientFailures, m.staleWrites)
			}
		})
	}
}

func TestStaleAndMismatchedSaves_CountStaleWrites(t *testing.T) {
	m := &recordingMetrics{}
	stale := playback.NewPgxQueueStateRepository(tagQuerier{tag: "INSERT 0 0"}, playback.WithQueueStateMetrics(m))
	if err := stale.Upsert(context.Background(), emptyQueueState(testUser())); !errors.Is(err, playback.ErrStaleQueueWrite) {
		t.Fatalf("Upsert = %v, want ErrStaleQueueWrite", err)
	}
	mismatch := playback.NewPgxQueueStateRepository(&positionQuerier{}, playback.WithQueueStateMetrics(m))
	if err := mismatch.UpdatePosition(context.Background(), testPosition(t)); !errors.Is(err, playback.ErrQueuePositionMismatch) {
		t.Fatalf("UpdatePosition = %v, want ErrQueuePositionMismatch", err)
	}
	if m.staleWrites != 2 {
		t.Fatalf("stale writes = %d, want 2", m.staleWrites)
	}
}
