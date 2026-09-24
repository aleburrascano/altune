package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.QueueStateRepository = (*PgxQueueStateRepository)(nil)

var queueStateOpTimeout = 3 * time.Second

type corruptStoredStateError struct {
	cause error
}

func (e *corruptStoredStateError) Error() string {
	return fmt.Sprintf("corrupt stored queue state: %v", e.cause)
}

// Is exposes the classification to callers through the port-level sentinel.
// It deliberately does not Unwrap to the cause: the cause is a domain
// validation error carrying a 400 status, and a stored-data fault must not be
// mistaken for a client error.
func (e *corruptStoredStateError) Is(target error) bool {
	return target == ports.ErrCorruptStoredState
}

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PgxQueueStateRepository struct {
	pool    querier
	metrics ports.QueueStateMetrics
}

func NewPgxQueueStateRepository(pool *pgxpool.Pool, opts ...func(*PgxQueueStateRepository)) *PgxQueueStateRepository {
	r := &PgxQueueStateRepository{pool: pool, metrics: ports.NoopQueueStateMetrics()}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithQueueStateMetrics injects the degradation-counter sink. Left as a
// functional option so the adapter stays constructible without a metrics
// backend (defaulting to a no-op).
func WithQueueStateMetrics(m ports.QueueStateMetrics) func(*PgxQueueStateRepository) {
	return func(r *PgxQueueStateRepository) {
		if m != nil {
			r.metrics = m
		}
	}
}

// recordTimeout counts an op that blew its per-op deadline. It attributes the
// timeout only when the parent context is still live: a caller-side cancel is
// the client leaving, not the database being slow, and must not inflate the
// health signal.
func (r *PgxQueueStateRepository) recordTimeout(parent context.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		r.metrics.QueueStateOpTimedOut()
	}
}

// runOp runs one database op under this repository's failure policy: the
// deadline it gets, the attribution of a blown one, and the line naming whose op
// failed. Sole owner of that policy, so each changes once for every op rather
// than once per call site.
//
// Such a fault reaches the request as a bare 500 logged with method and path
// alone (httputil.HandleServiceError), so user_id is the dimension an operator
// is missing: whose write was lost (#1595). It is also all the line carries
// beside the cause — the stored queue is the PII an erasure exists to remove
// (#1097), and a pgx error prints severity, message and SQLSTATE, never the
// bound values.
func (r *PgxQueueStateRepository) runOp(
	ctx context.Context,
	op string,
	userId shared.UserId,
	run func(opCtx context.Context) error,
) error {
	opCtx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	err := run(opCtx)
	r.recordTimeout(ctx, err)
	if isUnclassifiedFault(err) {
		slog.ErrorContext(ctx, "playback.queue_state_op_failed",
			"op", op, "user_id", userId.String(), "error", err)
	}
	if isTransientFault(err) {
		return fmt.Errorf("%w: %w", ports.ErrQueueStateUnavailable, err)
	}
	return err
}

func isTransientFault(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return transientSQLState(pgErr.Code)
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &connectErr) ||
		errors.As(err, &netErr) ||
		pgconn.SafeToRetry(err)
}

func transientSQLState(code string) bool {
	switch code {
	case "57P01", "57P02", "57P03", "40001", "40P01":
		return true
	}
	return strings.HasPrefix(code, "08") || strings.HasPrefix(code, "53")
}

// isUnclassifiedFault holds for a database failure this repository reports no
// other way — a refused connection, an exhausted pool, a violated constraint.
// The outcomes it already classifies are excluded: a blown deadline is counted
// (QueueStateOpTimedOut), a cancel is the client leaving, and no row is how an
// absent queue reads. Stale writes and corrupt state are classified from what
// the op returned, never from an error, so they never reach here.
func isUnclassifiedFault(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, pgx.ErrNoRows)
}

func (r *PgxQueueStateRepository) Upsert(ctx context.Context, state *domain.QueueState) error {
	// Re-validate at the persistence boundary: QueueState is an exported field
	// bag, so a struct-literal or mutation bypass could otherwise hand Upsert a
	// state the constructors never approved. Reject it before writing a row.
	if err := state.Validate(); err != nil {
		return err
	}

	// updated_at is the instant this save was handled, placed on the database's
	// clock: clock_timestamp() at execution minus how long ago the save was
	// stamped. Every API instance thus orders saves against one clock, and the
	// age is a monotonic duration, so neither a wall-clock step on an instance
	// nor skew between instances can reorder them. A save that was handled
	// earlier but reaches the database later (a slow pool wait, a delayed
	// statement) still carries its earlier instant and is rejected as stale.
	//
	// The same guard fences a save against an erasure (#1594), which is stamped
	// on that clock too and leaves the row behind rather than deleting it: a save
	// handled before the erasure loses to it exactly as it loses to a newer save,
	// and a save handled after it wins and clears the marker, so erasure orders
	// writes rather than locking the user out of saving again.
	//
	// A track list equal to the stored one keeps the stored datum instead of
	// the incoming copy (#1126). Postgres then reuses the out-of-line (TOAST)
	// value rather than writing it again, so a periodic autosave on a
	// max-length queue whose lists did not change writes a few hundred bytes of
	// WAL instead of ~880 KiB. The comparison only reads the stored arrays.
	var tag pgconn.CommandTag
	err := r.runOp(ctx, "upsert", state.UserId, func(opCtx context.Context) error {
		var err error
		tag, err = r.pool.Exec(opCtx,
			`INSERT INTO playback_queue_state (user_id, track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, clock_timestamp() - $9::bigint * interval '1 microsecond')
		 ON CONFLICT (user_id) DO UPDATE SET
		   track_ids = CASE WHEN playback_queue_state.track_ids = EXCLUDED.track_ids
		     THEN playback_queue_state.track_ids ELSE EXCLUDED.track_ids END,
		   current_idx = EXCLUDED.current_idx,
		   position_ms = EXCLUDED.position_ms,
		   shuffled = EXCLUDED.shuffled,
		   repeat_mode = EXCLUDED.repeat_mode,
		   source_id = EXCLUDED.source_id,
		   natural_order = CASE WHEN playback_queue_state.natural_order = EXCLUDED.natural_order
		     THEN playback_queue_state.natural_order ELSE EXCLUDED.natural_order END,
		   updated_at = EXCLUDED.updated_at,
		   erased_at = NULL
		 WHERE playback_queue_state.updated_at <= EXCLUDED.updated_at`,
			state.UserId.UUID(),
			state.TrackIds,
			state.CurrentIdx,
			state.PositionMs,
			state.Shuffled,
			state.RepeatMode.String(),
			state.SourceId,
			state.NaturalOrder,
			handlingAge{stampedAt: state.UpdatedAt},
		)
		return err
	})
	if err != nil {
		return err
	}
	// Zero rows means a guard rejected the write: either the stored snapshot was
	// handled later, or the user erased their queue after this save was handled.
	// Both mean a later event won and this save had no effect, and both must say
	// so rather than report a success that wrote nothing.
	if tag.RowsAffected() == 0 {
		return domain.ErrStaleQueueWrite
	}
	return nil
}

// UpdatePosition is the lighter save for the frequent position-only autosave
// (#1126): it binds no track list, so neither list is encoded, sent, compared
// or rewritten; the row's scalar columns and updated_at are all it writes.
//
// It applies only when the stored queue holds CurrentTrackId at CurrentIdx
// (Postgres arrays are 1-based, hence the +1), so a position can never land on
// a queue it was not measured against, and never creates a row. updated_at is
// placed on the database clock as Upsert places it, and the same "stored
// updated_at <= this save's" guard orders it against full saves in both
// directions: a position save handled before a newer full save is rejected as
// stale, and a full save handled before a newer position save is.
//
// That instant is statement_timestamp(), fixed when the statement starts, not
// clock_timestamp(), which reads now: with the lock below, now is after the
// wait on a concurrent writer, so a contended save's instant grew by however
// long it waited and could pass the very full save it waited for, letting
// older position data overwrite newer (#1578).
//
// Both guards and the classification read one row version: the `q` CTE locks
// the row, so a concurrent writer is waited out once, and what it committed is
// what both the UPDATE and the EXISTS see. Guarding against the table instead
// let the two disagree (#1570) — an UPDATE that waits on a lock re-checks its
// predicate against the newly committed row, while a separate EXISTS still
// reads the statement snapshot taken before the wait, so a save that lost its
// track to a concurrent full save was reported as merely stale (retry) rather
// than a position mismatch (resync).
func (r *PgxQueueStateRepository) UpdatePosition(ctx context.Context, position *domain.QueuePosition) error {
	if err := position.Validate(); err != nil {
		return err
	}

	var applied, matched bool
	err := r.runOp(ctx, "update_position", position.UserId, func(opCtx context.Context) error {
		return r.pool.QueryRow(opCtx,
			`WITH handled AS (
		   SELECT statement_timestamp() - $4::bigint * interval '1 microsecond' AS at
		 ), q AS (
		   SELECT user_id, track_ids, updated_at
		   FROM playback_queue_state
		   WHERE user_id = $1
		   FOR UPDATE
		 ), updated AS (
		   UPDATE playback_queue_state
		   SET current_idx = $2, position_ms = $3, updated_at = handled.at
		   FROM handled, q
		   WHERE playback_queue_state.user_id = q.user_id
		     AND q.track_ids[$2::int + 1] = $5
		     AND q.updated_at <= handled.at
		   RETURNING 1
		 )
		 SELECT EXISTS (SELECT 1 FROM updated),
		        EXISTS (SELECT 1 FROM q WHERE q.track_ids[$2::int + 1] = $5)`,
			position.UserId.UUID(),
			position.CurrentIdx,
			position.PositionMs,
			handlingAge{stampedAt: position.UpdatedAt},
			position.CurrentTrackId,
		).Scan(&applied, &matched)
	})
	if err != nil {
		return err
	}
	switch {
	case applied:
		return nil
	case matched:
		return domain.ErrStaleQueueWrite
	default:
		return domain.ErrQueuePositionMismatch
	}
}

// handlingAge binds, in whole microseconds, how long ago a save was stamped.
// pgx calls Value while encoding the statement, after a pooled connection has
// been acquired, so time spent waiting for a connection counts toward the age
// instead of making a delayed save look newer than it is. time.Since uses the
// stamp's monotonic reading when it has one (NewQueueState keeps it), so a
// wall-clock step between stamping and writing cannot distort the age. A stamp
// in the future (a fast clock, when there is no monotonic reading) clamps to
// zero: it must not place the row ahead of the database's clock and lock out
// every later save.
type handlingAge struct {
	stampedAt time.Time
}

func (a handlingAge) Value() (driver.Value, error) {
	age := time.Since(a.stampedAt)
	if age < 0 {
		age = 0
	}
	return age.Microseconds(), nil
}

func (r *PgxQueueStateRepository) GetForUser(
	ctx context.Context,
	userId shared.UserId,
) (*domain.QueueState, error) {
	var row scannedRow
	err := r.runOp(ctx, "get_for_user", userId, func(opCtx context.Context) error {
		return r.pool.QueryRow(opCtx,
			`SELECT track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order, updated_at
		 FROM playback_queue_state
		 WHERE user_id = $1 AND erased_at IS NULL`,
			userId.UUID(),
		).Scan(&row.trackIds, &row.currentIdx, &row.positionMs, &row.shuffled, &row.repeatMode, &row.sourceId, &row.naturalOrder, &row.updatedAt)
	})

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	state, err := hydrate(userId, row)
	if errors.Is(err, ports.ErrCorruptStoredState) {
		r.metrics.CorruptStoredState()
	}
	return state, err
}

// erasureFenceWindow is how long an erased row stays behind to reject the saves
// that predate it. It must outlive the oldest save that can still reach
// Postgres, and a request is already bounded by the API's 60s response deadline
// and this repository's 3s per-op deadline, so minutes is generous. Past it the
// row decides nothing, and a marker that outlives its purpose is a record of a
// possibly deleted account nobody asked us to keep.
const erasureFenceWindow = 15 * time.Minute

// DeleteForUser blanks every stored column in place and stamps the row erased
// rather than deleting it, because the row is what orders the saves still in
// flight against the erasure (#1594). Against a deleted row a save has nothing
// to conflict with — not even when it is already blocked on the erasure's own
// lock, where re-checking the guard against the committed row is the only thing
// that can reveal the erasure — so it inserts, and the erased queue is back.
//
// The stamp is the database clock at execution rather than the instant Forget
// was handled, and the write carries no guard of its own: an erasure wins over
// whatever is stored, including a save that commits while it waits. It inserts
// where no row existed for the same reason — a save for a user with nothing
// stored is exactly the one that would otherwise land after the erasure and
// become the stored queue of an erased account.
//
// Cost: one partial-index scan to reap the rows erased before the window, at
// most the deleted-identity sweep's batch plus one window of self-service
// erasures, each deleted by primary key.
func (r *PgxQueueStateRepository) DeleteForUser(ctx context.Context, userId shared.UserId) error {
	return r.runOp(ctx, "delete_for_user", userId, func(opCtx context.Context) error {
		_, err := r.pool.Exec(opCtx,
			`WITH reaped AS (
		   DELETE FROM playback_queue_state
		   WHERE user_id <> $1
		     AND erased_at < clock_timestamp() - $2::bigint * interval '1 second'
		 )
		 INSERT INTO playback_queue_state (user_id, updated_at, erased_at)
		 VALUES ($1, clock_timestamp(), clock_timestamp())
		 ON CONFLICT (user_id) DO UPDATE SET
		   track_ids = '{}',
		   natural_order = '{}',
		   source_id = '',
		   current_idx = 0,
		   position_ms = 0,
		   shuffled = FALSE,
		   repeat_mode = $3,
		   updated_at = EXCLUDED.updated_at,
		   erased_at = EXCLUDED.erased_at`,
			userId.UUID(),
			int64(erasureFenceWindow.Seconds()),
			domain.RepeatOff.String(),
		)
		return err
	})
}

type scannedRow struct {
	trackIds     []string
	currentIdx   int
	positionMs   int64
	shuffled     bool
	repeatMode   string
	sourceId     string
	naturalOrder []string
	updatedAt    time.Time
}

func hydrate(userId shared.UserId, row scannedRow) (*domain.QueueState, error) {
	rm, err := domain.ParseRepeatMode(row.repeatMode)
	if err != nil {
		return nil, &corruptStoredStateError{cause: err}
	}

	state, err := domain.RehydrateQueueState(domain.QueueStateInput{
		UserId:       userId,
		TrackIds:     row.trackIds,
		CurrentIdx:   row.currentIdx,
		PositionMs:   row.positionMs,
		Shuffled:     row.shuffled,
		RepeatMode:   rm,
		SourceId:     row.sourceId,
		NaturalOrder: row.naturalOrder,
	}, row.updatedAt)
	if err != nil {
		return nil, &corruptStoredStateError{cause: err}
	}
	return state, nil
}
