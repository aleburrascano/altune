package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

var _ ports.QueueStateRepository = (*PgxQueueStateRepository)(nil)

var queueStateOpTimeout = 3 * time.Second

type corruptStoredStateError struct {
	cause error
}

func (e *corruptStoredStateError) Error() string {
	return fmt.Sprintf("corrupt stored queue state: %v", e.cause)
}

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PgxQueueStateRepository struct {
	pool querier
}

func NewPgxQueueStateRepository(pool *pgxpool.Pool) *PgxQueueStateRepository {
	return &PgxQueueStateRepository{pool: pool}
}

func (r *PgxQueueStateRepository) Upsert(ctx context.Context, state *domain.QueueState) error {
	ctx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO playback_queue_state (user_id, track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (user_id) DO UPDATE SET
		   track_ids = EXCLUDED.track_ids,
		   current_idx = EXCLUDED.current_idx,
		   position_ms = EXCLUDED.position_ms,
		   shuffled = EXCLUDED.shuffled,
		   repeat_mode = EXCLUDED.repeat_mode,
		   source_id = EXCLUDED.source_id,
		   natural_order = EXCLUDED.natural_order,
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
		state.UpdatedAt,
	)
	return err
}

func (r *PgxQueueStateRepository) GetForUser(
	ctx context.Context,
	userId shared.UserId,
) (*domain.QueueState, error) {
	ctx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	var row scannedRow
	err := r.pool.QueryRow(ctx,
		`SELECT track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, natural_order, updated_at
		 FROM playback_queue_state
		 WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&row.trackIds, &row.currentIdx, &row.positionMs, &row.shuffled, &row.repeatMode, &row.sourceId, &row.naturalOrder, &row.updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return hydrate(userId, row)
}

func (r *PgxQueueStateRepository) DeleteForUser(ctx context.Context, userId shared.UserId) error {
	ctx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`DELETE FROM playback_queue_state WHERE user_id = $1`,
		userId.UUID(),
	)
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
