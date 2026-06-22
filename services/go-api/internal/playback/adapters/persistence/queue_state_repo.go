package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

var _ ports.QueueStateRepository = (*PgxQueueStateRepository)(nil)

type PgxQueueStateRepository struct {
	pool *pgxpool.Pool
}

func NewPgxQueueStateRepository(pool *pgxpool.Pool) *PgxQueueStateRepository {
	return &PgxQueueStateRepository{pool: pool}
}

func (r *PgxQueueStateRepository) Upsert(ctx context.Context, state *domain.QueueState) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO playback_queue_state (user_id, track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (user_id) DO UPDATE SET
		   track_ids = EXCLUDED.track_ids,
		   current_idx = EXCLUDED.current_idx,
		   position_ms = EXCLUDED.position_ms,
		   shuffled = EXCLUDED.shuffled,
		   repeat_mode = EXCLUDED.repeat_mode,
		   source_id = EXCLUDED.source_id,
		   updated_at = EXCLUDED.updated_at`,
		state.UserId.UUID(),
		state.TrackIds,
		state.CurrentIdx,
		state.PositionMs,
		state.Shuffled,
		state.RepeatMode.String(),
		state.SourceId,
		state.UpdatedAt,
	)
	return err
}

func (r *PgxQueueStateRepository) GetForUser(
	ctx context.Context,
	userId shared.UserId,
) (*domain.QueueState, error) {
	var (
		trackIds   []string
		currentIdx int
		positionMs int64
		shuffled   bool
		repeatMode string
		sourceId   string
		updatedAt  time.Time
	)

	err := r.pool.QueryRow(ctx,
		`SELECT track_ids, current_idx, position_ms, shuffled, repeat_mode, source_id, updated_at
		 FROM playback_queue_state
		 WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&trackIds, &currentIdx, &positionMs, &shuffled, &repeatMode, &sourceId, &updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rm, err := domain.ParseRepeatMode(repeatMode)
	if err != nil {
		return nil, fmt.Errorf("parse repeat mode: %w", err)
	}

	state, err := domain.RehydrateQueueState(userId, trackIds, currentIdx, positionMs, shuffled, rm, sourceId, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("rehydrate queue state: %w", err)
	}
	return state, nil
}
