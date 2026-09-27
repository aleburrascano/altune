package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const acquisitionJobChannel = "acquisition_jobs"

type PgxJobQueue struct {
	pool *pgxpool.Pool
}

var _ ports.JobQueue = (*PgxJobQueue)(nil)

func NewPgxJobQueue(pool *pgxpool.Pool) *PgxJobQueue {
	return &PgxJobQueue{pool: pool}
}

const enqueueJobSQL = `
UPDATE tracks
SET acquisition_status = 'pending',
	acquisition_job_kind = $2,
	acquisition_available_at = $3,
	acquisition_lease_until = CASE
		WHEN acquisition_status = 'pending' AND acquisition_lease_until >= now() THEN acquisition_lease_until
	END,
	failure_reason = ''
WHERE id = $1`

func (q *PgxJobQueue) Enqueue(ctx context.Context, trackID domain.TrackId, kind ports.JobKind, availableAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, enqueueJobSQL, trackID.UUID(), string(kind), availableAt)
	if err != nil {
		return fmt.Errorf("enqueue acquisition job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("enqueue acquisition job: track %s not found", trackID)
	}
	if _, err := q.pool.Exec(ctx, "NOTIFY "+acquisitionJobChannel); err != nil {
		return fmt.Errorf("notify acquisition job: %w", err)
	}
	return nil
}

const claimJobSQL = `
WITH candidate AS (
	SELECT t.id
	FROM tracks t
	WHERE t.acquisition_status = 'pending'
		AND t.acquisition_available_at <= now()
		AND (t.acquisition_lease_until IS NULL OR t.acquisition_lease_until < now())
	ORDER BY (
			SELECT count(*) FROM tracks u
			WHERE u.user_id = t.user_id
				AND u.acquisition_status = 'pending'
				AND u.acquisition_lease_until >= now()
		), t.acquisition_available_at
	FOR UPDATE OF t SKIP LOCKED
	LIMIT 1
)
UPDATE tracks
SET acquisition_lease_until = now() + make_interval(secs => $1),
	acquisition_attempts = acquisition_attempts + 1
FROM candidate
WHERE tracks.id = candidate.id
RETURNING tracks.id, tracks.user_id, tracks.acquisition_job_kind, tracks.acquisition_attempts`

func (q *PgxJobQueue) Claim(ctx context.Context, lease time.Duration) (ports.Job, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var (
		trackID  domain.TrackId
		userID   shared.UserId
		kind     string
		attempts int
	)
	row := q.pool.QueryRow(ctx, claimJobSQL, lease.Seconds())
	err := scanTrackClaim(row, &trackID, &userID, &kind, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.Job{}, ports.ErrNoJobAvailable
	}
	if err != nil {
		return ports.Job{}, fmt.Errorf("claim acquisition job: %w", err)
	}
	return ports.Job{TrackID: trackID, UserID: userID, Kind: ports.JobKind(kind), Attempts: attempts}, nil
}

func scanTrackClaim(row pgx.Row, trackID *domain.TrackId, userID *shared.UserId, kind *string, attempts *int) error {
	var rawTrackID, rawUserID uuid.UUID
	if err := row.Scan(&rawTrackID, &rawUserID, kind, attempts); err != nil {
		return err
	}
	*trackID = domain.TrackIdFromUUID(rawTrackID)
	*userID = shared.NewUserId(rawUserID)
	return nil
}

const heartbeatJobSQL = `
UPDATE tracks
SET acquisition_lease_until = now() + make_interval(secs => $3)
WHERE id = $1 AND acquisition_attempts = $2 AND acquisition_status = 'pending' AND acquisition_lease_until >= now()`

func (q *PgxJobQueue) Heartbeat(ctx context.Context, trackID domain.TrackId, fence int, lease time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, heartbeatJobSQL, trackID.UUID(), fence, lease.Seconds())
	if err != nil {
		return fmt.Errorf("heartbeat acquisition job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("heartbeat acquisition job for track %s: %w", trackID, ports.ErrLeaseLost)
	}
	return nil
}

const releaseJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL, acquisition_available_at = $3
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PgxJobQueue) Release(ctx context.Context, trackID domain.TrackId, fence int, availableAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, releaseJobSQL, trackID.UUID(), fence, availableAt)
	if err != nil {
		return fmt.Errorf("release acquisition job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("release acquisition job for track %s: %w", trackID, ports.ErrLeaseLost)
	}
	return nil
}

const settleJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL,
	acquisition_available_at = CASE
		WHEN acquisition_status = 'pending' THEN COALESCE(acquisition_available_at, now())
	END
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PgxJobQueue) Settle(ctx context.Context, trackID domain.TrackId, fence int) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, settleJobSQL, trackID.UUID(), fence)
	if err != nil {
		return fmt.Errorf("settle acquisition job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("settle acquisition job for track %s: %w", trackID, ports.ErrLeaseLost)
	}
	return nil
}
