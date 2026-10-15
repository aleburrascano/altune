package persistence

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
var _ ports.QueueDepthReader = (*PgxJobQueue)(nil)
var _ ports.UpgradeJobQueue = (*PgxJobQueue)(nil)

func NewPgxJobQueue(pool *pgxpool.Pool) *PgxJobQueue {
	return &PgxJobQueue{pool: pool}
}

const enqueueJobSQL = `
UPDATE tracks
SET acquisition_status = CASE
		WHEN $2 IN ('replace', 'upgrade') AND acquisition_status = 'ready' THEN acquisition_status
		ELSE 'pending'
	END,
	acquisition_job_kind = $2,
	acquisition_available_at = now() + make_interval(secs => $3),
	acquisition_lease_until = CASE
		WHEN acquisition_job_kind = 'upgrade' AND $2 <> 'upgrade' THEN NULL
		WHEN (acquisition_status = 'pending' OR acquisition_available_at IS NOT NULL)
			AND acquisition_lease_until >= now() THEN acquisition_lease_until
	END,
	acquisition_attempts = acquisition_attempts
		+ CASE WHEN acquisition_job_kind = 'upgrade' AND $2 <> 'upgrade' THEN 1 ELSE 0 END,
	acquisition_run_attempts = 0,
	failure_reason = ''
WHERE id = $1
	AND (
		$2 <> 'upgrade'
		OR (acquisition_status = 'ready' AND (acquisition_available_at IS NULL OR acquisition_job_kind = 'upgrade'))
	)
	AND (
		(acquisition_job_kind = 'upgrade' AND $2 <> 'upgrade')
		OR (acquisition_status <> 'pending' AND acquisition_available_at IS NULL)
		OR acquisition_lease_until IS NULL
		OR acquisition_lease_until < now()
		OR acquisition_job_kind = $2
	)`

const jobKindForLeasedTrackSQL = `SELECT acquisition_job_kind FROM tracks WHERE id = $1`

func (q *PgxJobQueue) Enqueue(ctx context.Context, trackID shared.TrackId, kind ports.JobKind, delay time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, enqueueJobSQL, trackID.UUID(), string(kind), delay.Seconds())
	if err != nil {
		return fmt.Errorf("enqueue acquisition job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return q.resolveEnqueueNoop(ctx, trackID, kind)
	}
	if _, err := q.pool.Exec(ctx, "NOTIFY "+acquisitionJobChannel); err != nil {
		slog.WarnContext(ctx, "acquisition.enqueue_notify_failed",
			"track_id", trackID.String(), "error", err)
	}
	return nil
}

func (q *PgxJobQueue) resolveEnqueueNoop(ctx context.Context, trackID shared.TrackId, kind ports.JobKind) error {
	var runningKind string
	err := q.pool.QueryRow(ctx, jobKindForLeasedTrackSQL, trackID.UUID()).Scan(&runningKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("enqueue acquisition job: track %s not found", trackID)
	}
	if err != nil {
		return fmt.Errorf("enqueue acquisition job: %w", err)
	}
	if ports.JobKind(runningKind) != kind {
		return acqdomain.ErrJobKindConflict
	}
	return nil
}

const pendingDepthSQL = `
SELECT count(*),
	COALESCE(EXTRACT(EPOCH FROM now() - min(acquisition_available_at) FILTER (WHERE acquisition_available_at <= now())), 0)::float8
FROM tracks
WHERE acquisition_available_at IS NOT NULL
	AND (acquisition_status = 'pending' OR acquisition_job_kind = 'replace')
	AND (acquisition_lease_until IS NULL OR acquisition_lease_until < now())`

func (q *PgxJobQueue) PendingDepth(ctx context.Context) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var (
		pending    int
		oldestSecs float64
	)
	if err := q.pool.QueryRow(ctx, pendingDepthSQL).Scan(&pending, &oldestSecs); err != nil {
		return 0, 0, fmt.Errorf("read acquisition pending depth: %w", err)
	}
	return pending, time.Duration(oldestSecs * float64(time.Second)), nil
}

const claimJobSQL = `
WITH candidate AS (
	SELECT t.id
	FROM tracks t
	WHERE t.acquisition_available_at <= now()
		AND t.acquisition_job_kind <> 'upgrade'
		AND (t.acquisition_status = 'pending' OR t.acquisition_job_kind = 'replace')
		AND (t.acquisition_lease_until IS NULL OR t.acquisition_lease_until < now())
	ORDER BY (
			SELECT count(*) FROM tracks u
			WHERE u.user_id = t.user_id
				AND u.acquisition_available_at IS NOT NULL
				AND u.acquisition_lease_until >= now()
		), t.acquisition_available_at
	FOR UPDATE OF t SKIP LOCKED
	LIMIT 1
)
` + leaseClaimedJobSQL

const claimUpgradeJobSQL = `
WITH candidate AS (
	SELECT t.id
	FROM tracks t
	WHERE t.acquisition_available_at <= now()
		AND t.acquisition_job_kind = 'upgrade'
		AND (t.acquisition_lease_until IS NULL OR t.acquisition_lease_until < now())
		AND NOT EXISTS (
			SELECT 1 FROM tracks u
			WHERE u.acquisition_available_at <= now()
				AND u.acquisition_job_kind <> 'upgrade'
				AND (u.acquisition_status = 'pending' OR u.acquisition_job_kind = 'replace')
				AND (u.acquisition_lease_until IS NULL OR u.acquisition_lease_until < now())
		)
	ORDER BY t.acquisition_available_at
	FOR UPDATE OF t SKIP LOCKED
	LIMIT 1
)
` + leaseClaimedJobSQL

const leaseClaimedJobSQL = `UPDATE tracks
SET acquisition_lease_until = now() + make_interval(secs => $1),
	acquisition_attempts = acquisition_attempts + 1,
	acquisition_run_attempts = acquisition_run_attempts + 1,
	acquisition_started_at = now()
FROM candidate
WHERE tracks.id = candidate.id
RETURNING tracks.id, tracks.user_id, tracks.acquisition_job_kind, tracks.acquisition_attempts, tracks.acquisition_run_attempts,
	GREATEST(EXTRACT(EPOCH FROM now() - tracks.acquisition_available_at), 0)::float8`

func (q *PgxJobQueue) Claim(ctx context.Context, lease time.Duration) (ports.Job, error) {
	return q.claim(ctx, claimJobSQL, lease)
}

func (q *PgxJobQueue) ClaimUpgrade(ctx context.Context, lease time.Duration) (ports.Job, error) {
	return q.claim(ctx, claimUpgradeJobSQL, lease)
}

func (q *PgxJobQueue) claim(ctx context.Context, sql string, lease time.Duration) (ports.Job, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	job, err := scanClaimedJob(q.pool.QueryRow(ctx, sql, lease.Seconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.Job{}, acqdomain.ErrNoJobAvailable
	}
	if err != nil {
		return ports.Job{}, fmt.Errorf("claim acquisition job: %w", err)
	}
	return job, nil
}

func scanClaimedJob(row pgx.Row) (ports.Job, error) {
	var (
		trackID  shared.TrackId
		userID   shared.UserId
		kind     string
		attempts int
		run      int
		waited   float64
	)
	if err := scanTrackClaim(row, &trackID, &userID, &kind, &attempts, &run, &waited); err != nil {
		return ports.Job{}, err
	}
	return ports.Job{
		TrackID: trackID, UserID: userID, Kind: ports.JobKind(kind), Attempts: attempts, Run: run, Fence: ports.Fence(attempts),
		Waited: time.Duration(waited * float64(time.Second)),
	}, nil
}

func scanTrackClaim(row pgx.Row, trackID *shared.TrackId, userID *shared.UserId, kind *string, attempts, run *int, waited *float64) error {
	var rawTrackID, rawUserID uuid.UUID
	if err := row.Scan(&rawTrackID, &rawUserID, kind, attempts, run, waited); err != nil {
		return err
	}
	*trackID = shared.TrackIdFromUUID(rawTrackID)
	*userID = shared.NewUserId(rawUserID)
	return nil
}

func (q *PgxJobQueue) execFenced(ctx context.Context, trackID shared.TrackId, op, sql string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("%s acquisition job: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s acquisition job for track %s: %w", op, trackID, acqdomain.ErrLeaseLost)
	}
	return nil
}

const heartbeatJobSQL = `
UPDATE tracks
SET acquisition_lease_until = now() + make_interval(secs => $3)
WHERE id = $1 AND acquisition_attempts = $2 AND acquisition_available_at IS NOT NULL AND acquisition_lease_until >= now()`

func (q *PgxJobQueue) Heartbeat(ctx context.Context, trackID shared.TrackId, fence ports.Fence, lease time.Duration) error {
	return q.execFenced(ctx, trackID, "heartbeat", heartbeatJobSQL, trackID.UUID(), int(fence), lease.Seconds())
}

const releaseJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL, acquisition_available_at = now() + make_interval(secs => $3)
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PgxJobQueue) Release(ctx context.Context, trackID shared.TrackId, fence ports.Fence, delay time.Duration) error {
	return q.execFenced(ctx, trackID, "release", releaseJobSQL, trackID.UUID(), int(fence), delay.Seconds())
}

const settleJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL,
	acquisition_available_at = CASE
		WHEN acquisition_status = 'pending' THEN COALESCE(acquisition_available_at, now())
	END
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PgxJobQueue) Settle(ctx context.Context, trackID shared.TrackId, fence ports.Fence) error {
	return q.execFenced(ctx, trackID, "settle", settleJobSQL, trackID.UUID(), int(fence))
}

const notifierReconnectDelay = time.Second

type PgxJobNotifier struct {
	pool *pgxpool.Pool
}

var _ ports.JobNotifier = (*PgxJobNotifier)(nil)

func NewPgxJobNotifier(pool *pgxpool.Pool) *PgxJobNotifier {
	return &PgxJobNotifier{pool: pool}
}

func (n *PgxJobNotifier) Listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		n.listenOnce(ctx, wake)
		select {
		case <-time.After(notifierReconnectDelay):
		case <-ctx.Done():
			return
		}
	}
}

func (n *PgxJobNotifier) listenOnce(ctx context.Context, wake chan<- struct{}) {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+acquisitionJobChannel); err != nil {
		return
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
