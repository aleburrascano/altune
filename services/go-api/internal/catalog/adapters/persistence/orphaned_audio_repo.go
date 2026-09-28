package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const undefinedTableCode = "42P01"

var _ ports.OrphanedAudioQueue = (*PgxOrphanedAudioRepository)(nil)

type PgxOrphanedAudioRepository struct {
	pool pgxPool
}

func NewPgxOrphanedAudioRepository(pool *pgxpool.Pool) *PgxOrphanedAudioRepository {
	return &PgxOrphanedAudioRepository{pool: pool}
}

func (r *PgxOrphanedAudioRepository) RecordOrphanedAudio(ctx context.Context, orphan ports.OrphanedAudio) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO orphaned_audio (audio_ref, user_id, track_id, recorded_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (audio_ref) DO UPDATE
		 SET user_id = EXCLUDED.user_id, track_id = EXCLUDED.track_id,
		     recorded_at = EXCLUDED.recorded_at, attempts = 0,
		     last_attempt_at = NULL, last_error = NULL`,
		orphan.AudioRef, orphan.UserId.UUID(), orphan.TrackId.UUID(),
	)
	return orphanQueueErr("record orphaned audio", err)
}

func (r *PgxOrphanedAudioRepository) ListOrphanedAudio(ctx context.Context, limit int) ([]ports.OrphanedAudio, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT audio_ref, user_id, track_id, recorded_at, attempts
		 FROM orphaned_audio
		 ORDER BY last_attempt_at ASC NULLS FIRST, recorded_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, orphanQueueErr("list orphaned audio", err)
	}
	defer rows.Close()

	var out []ports.OrphanedAudio
	for rows.Next() {
		var (
			o       ports.OrphanedAudio
			userID  uuid.UUID
			trackID uuid.UUID
		)
		if err := rows.Scan(&o.AudioRef, &userID, &trackID, &o.RecordedAt, &o.Attempts); err != nil {
			return nil, fmt.Errorf("scan orphaned audio: %w", err)
		}
		o.UserId = shared.NewUserId(userID)
		o.TrackId = domain.TrackIdFromUUID(trackID)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, orphanQueueErr("list orphaned audio", err)
	}
	return out, nil
}

func (r *PgxOrphanedAudioRepository) AudioUsage(ctx context.Context, audioRef string, owner shared.UserId) (ports.AudioUsage, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var referenced, acquiring bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tracks WHERE audio_ref = $1),
		        EXISTS (SELECT 1 FROM tracks WHERE user_id = $2 AND acquisition_status = $3)`,
		audioRef, owner.UUID(), domain.AcquisitionPending.String(),
	).Scan(&referenced, &acquiring)
	if err != nil {
		return ports.AudioReferenced, fmt.Errorf("check audio usage: %w", err)
	}
	switch {
	case referenced:
		return ports.AudioReferenced, nil
	case acquiring:
		return ports.AudioOwnerAcquiring, nil
	}
	return ports.AudioUnused, nil
}

func (r *PgxOrphanedAudioRepository) ResolveOrphanedAudio(ctx context.Context, audioRef string) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx, `DELETE FROM orphaned_audio WHERE audio_ref = $1`, audioRef)
	return orphanQueueErr("resolve orphaned audio", err)
}

func (r *PgxOrphanedAudioRepository) MarkOrphanedAudioAttempt(ctx context.Context, audioRef, cause string) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`UPDATE orphaned_audio
		 SET attempts = attempts + 1, last_attempt_at = now(), last_error = $2
		 WHERE audio_ref = $1`,
		audioRef, cause,
	)
	return orphanQueueErr("mark orphaned audio attempt", err)
}

func orphanQueueErr(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == undefinedTableCode {
		return fmt.Errorf("%s: %w: %w", op, ports.ErrOrphanedAudioQueueUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}
