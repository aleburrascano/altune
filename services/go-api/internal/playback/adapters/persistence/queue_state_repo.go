package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
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

func (r *PgxQueueStateRepository) Upsert(ctx context.Context, state *domain.QueueState) error {
	// Re-validate at the persistence boundary: QueueState is an exported field
	// bag, so a struct-literal or mutation bypass could otherwise hand Upsert a
	// state the constructors never approved. Reject it before writing a row.
	if err := state.Validate(); err != nil {
		return err
	}

	opCtx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	// updated_at is the instant this save was handled, placed on the database's
	// clock: clock_timestamp() at execution minus how long ago the save was
	// stamped. Every API instance thus orders saves against one clock, and the
	// age is a monotonic duration, so neither a wall-clock step on an instance
	// nor skew between instances can reorder them. A save that was handled
	// earlier but reaches the database later (a slow pool wait, a delayed
	// statement) still carries its earlier instant and is rejected as stale.
	//
	// A track list equal to the stored one keeps the stored datum instead of
	// the incoming copy (#1126). Postgres then reuses the out-of-line (TOAST)
	// value rather than writing it again, so a periodic autosave on a
	// max-length queue whose lists did not change writes a few hundred bytes of
	// WAL instead of ~880 KiB. The comparison only reads the stored arrays.
	tag, err := r.pool.Exec(opCtx,
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
		   updated_at = EXCLUDED.updated_at
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
	if err != nil {
		r.recordTimeout(ctx, err)
		return err
	}
	// Zero rows means the ON CONFLICT ... WHERE guard rejected the update: the
	// stored snapshot was handled later, so this save had no effect and must
	// say so.
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
// placed on the database clock exactly as Upsert places it, and the same
// "stored updated_at <= this save's" guard orders it against full saves in
// both directions: a position save handled before a newer full save is
// rejected as stale, and a full save handled before a newer position save is.
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

	opCtx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	var applied, matched bool
	err := r.pool.QueryRow(opCtx,
		`WITH handled AS (
		   SELECT clock_timestamp() - $4::bigint * interval '1 microsecond' AS at
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
	if err != nil {
		r.recordTimeout(ctx, err)
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
	opCtx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	var row scannedRow
	err := r.pool.QueryRow(opCtx,
		`SELECT track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order, updated_at
		 FROM playback_queue_state
		 WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&row.trackIds, &row.currentIdx, &row.positionMs, &row.shuffled, &row.repeatMode, &row.sourceId, &row.naturalOrder, &row.updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		r.recordTimeout(ctx, err)
		return nil, err
	}

	state, err := hydrate(userId, row)
	if errors.Is(err, ports.ErrCorruptStoredState) {
		r.metrics.CorruptStoredState()
	}
	return state, err
}

func (r *PgxQueueStateRepository) DeleteForUser(ctx context.Context, userId shared.UserId) error {
	opCtx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	_, err := r.pool.Exec(opCtx,
		`DELETE FROM playback_queue_state WHERE user_id = $1`,
		userId.UUID(),
	)
	if err != nil {
		r.recordTimeout(ctx, err)
	}
	return err
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
