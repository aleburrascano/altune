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

func WithQueueStateMetrics(m ports.QueueStateMetrics) func(*PgxQueueStateRepository) {
	return func(r *PgxQueueStateRepository) {
		if m != nil {
			r.metrics = m
		}
	}
}

func (r *PgxQueueStateRepository) recordTimeout(parent context.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		r.metrics.QueueStateOpTimedOut()
	}
}

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

func (r *PgxQueueStateRepository) Upsert(ctx context.Context, state *domain.QueueState) error {
	if err := state.Validate(); err != nil {
		return err
	}

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
		 WHERE playback_queue_state.updated_at <= EXCLUDED.updated_at
		   AND (playback_queue_state.erased_at IS NULL
		     OR playback_queue_state.erased_at + $10::bigint * interval '1 microsecond' < EXCLUDED.updated_at)`,
			state.UserId.UUID(),
			state.TrackIds,
			state.CurrentIdx,
			state.PositionMs,
			state.Shuffled,
			state.RepeatMode.String(),
			state.SourceId,
			state.NaturalOrder,
			handlingAge{stampedAt: state.UpdatedAt},
			erasureSaveGrace.Microseconds(),
		)
		return err
	})
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStaleQueueWrite
	}
	return nil
}

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

const erasureFenceWindow = 15 * time.Minute

var erasureSaveGrace = queueStateOpTimeout

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

func (r *PgxQueueStateRepository) ReapErasedStates(ctx context.Context) (int64, error) {
	var reaped int64
	err := r.runOp(ctx, "reap_erased_states", shared.UserId{}, func(opCtx context.Context) error {
		tag, err := r.pool.Exec(opCtx,
			`DELETE FROM playback_queue_state
		 WHERE erased_at < clock_timestamp() - $1::bigint * interval '1 second'`,
			int64(erasureFenceWindow.Seconds()))
		reaped = tag.RowsAffected()
		return err
	})
	return reaped, err
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
