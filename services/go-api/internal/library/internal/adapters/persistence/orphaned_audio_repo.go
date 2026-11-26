package persistence

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const undefinedTableCode = "42P01"

var _ ports.OrphanedAudioQueue = (*PostgresOrphanedAudioRepository)(nil)

type PostgresOrphanedAudioRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresOrphanedAudioRepository(pool *pgxpool.Pool) *PostgresOrphanedAudioRepository {
	return &PostgresOrphanedAudioRepository{pool: pool}
}

func (r *PostgresOrphanedAudioRepository) RecordOrphanedAudio(ctx context.Context, orphan ports.OrphanedAudio) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO orphaned_audio (audio_ref, user_id, track_id, recorded_at, not_before)
		 VALUES ($1, $2, $3, now(), COALESCE($4, now()))
		 ON CONFLICT (audio_ref) DO UPDATE
		 SET user_id = EXCLUDED.user_id, track_id = EXCLUDED.track_id,
		     recorded_at = EXCLUDED.recorded_at, not_before = EXCLUDED.not_before, attempts = 0,
		     last_attempt_at = NULL, last_error = NULL`,
		orphan.AudioRef, orphan.UserId.UUID(), orphan.TrackId.UUID(), notBeforeArg(orphan.NotBefore),
	)
	return orphanQueueErr("record orphaned audio", err)
}

func (r *PostgresOrphanedAudioRepository) ListOrphanedAudio(ctx context.Context, limit int) ([]ports.OrphanedAudio, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT audio_ref, user_id, track_id, recorded_at, attempts
		 FROM orphaned_audio
		 WHERE not_before <= now()
		 ORDER BY last_attempt_at ASC NULLS FIRST, recorded_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, orphanQueueErr("list orphaned audio", err)
	}
	out, err := pgx.CollectRows(rows, scanOrphanedAudio)
	if err != nil {
		return nil, orphanQueueErr("list orphaned audio", err)
	}
	return out, nil
}

func scanOrphanedAudio(row pgx.CollectableRow) (ports.OrphanedAudio, error) {
	var (
		o       ports.OrphanedAudio
		userID  uuid.UUID
		trackID uuid.UUID
	)
	if err := row.Scan(&o.AudioRef, &userID, &trackID, &o.RecordedAt, &o.Attempts); err != nil {
		return ports.OrphanedAudio{}, fmt.Errorf("scan orphaned audio: %w", err)
	}
	o.UserId = ids.NewUserId(userID)
	o.TrackId = domain.TrackIdFromUUID(trackID)
	return o, nil
}

func (r *PostgresOrphanedAudioRepository) AudioUsage(ctx context.Context, audioRef string, owner ids.UserId) (ports.AudioUsage, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var referenced, acquiring bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tracks WHERE audio_ref = $1),
		        EXISTS (SELECT 1 FROM tracks WHERE user_id = $2 AND acquisition_status = $3)`,
		audioRef, owner.UUID(), domain.AcquisitionPending.String(),
	).Scan(&referenced, &acquiring)
	if err != nil {
		return ports.AudioReferenced, fmt.Errorf("check audio usage: %w", classifyDBError(err))
	}
	switch {
	case referenced:
		return ports.AudioReferenced, nil
	case acquiring:
		return ports.AudioOwnerAcquiring, nil
	}
	return ports.AudioUnused, nil
}

func (r *PostgresOrphanedAudioRepository) ResolveOrphanedAudio(ctx context.Context, audioRef string) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx, `DELETE FROM orphaned_audio WHERE audio_ref = $1`, audioRef)
	return orphanQueueErr("resolve orphaned audio", err)
}

func (r *PostgresOrphanedAudioRepository) MarkOrphanedAudioAttempt(ctx context.Context, audioRef, cause string) error {
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

func notBeforeArg(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func orphanQueueErr(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == undefinedTableCode {
		return fmt.Errorf("%s: %w: %w", op, ports.ErrOrphanedAudioQueueUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, classifyDBError(err))
}
