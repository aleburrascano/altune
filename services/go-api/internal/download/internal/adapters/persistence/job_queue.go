package persistence

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
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

type PostgresJobQueue struct {
	pool *pgxpool.Pool
}

var (
	_ ports.DownloadQueue    = (*PostgresJobQueue)(nil)
	_ ports.QueueDepthReader = (*PostgresJobQueue)(nil)
	_ ports.UpgradeJobQueue  = (*PostgresJobQueue)(nil)
)

func NewPostgresJobQueue(pool *pgxpool.Pool) *PostgresJobQueue {
	return &PostgresJobQueue{pool: pool}
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
		+ CASE
			WHEN acquisition_job_kind = 'upgrade' AND $2 <> 'upgrade' THEN 1
			WHEN acquisition_lease_until < now() THEN 1
			ELSE 0
		END,
	acquisition_run_attempts = 0,
	acquisition_correlation_id = NULLIF($4, ''),
	failure_reason = '',
	version = version + 1
WHERE id = $1
	AND NOT ($2 = 'acquire' AND acquisition_status = 'ready')
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

const jobStateForRefusedEnqueueSQL = `
SELECT acquisition_job_kind, acquisition_status, COALESCE(acquisition_lease_until >= now(), false)
FROM tracks WHERE id = $1`

func (q *PostgresJobQueue) Enqueue(ctx context.Context, trackID ids.TrackId, kind ports.JobKind, delay time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	applied, err := q.applyEnqueue(ctx, trackID, kind, delay)
	if err != nil {
		return err
	}
	if !applied {
		if applied, err = q.resolveRefusedEnqueue(ctx, trackID, kind, delay); err != nil || !applied {
			return err
		}
	}
	if _, err := q.pool.Exec(ctx, "NOTIFY "+acquisitionJobChannel); err != nil {
		slog.WarnContext(ctx, "acquisition.enqueue_notify_failed",
			"track_id", trackID.String(), "error", err)
	}
	return nil
}

func (q *PostgresJobQueue) applyEnqueue(ctx context.Context, trackID ids.TrackId, kind ports.JobKind, delay time.Duration) (bool, error) {
	tag, err := q.pool.Exec(ctx, enqueueJobSQL, trackID.UUID(), string(kind), delay.Seconds(), logging.CorrelationIDFromContext(ctx))
	if err != nil {
		return false, fmt.Errorf("enqueue acquisition job: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (q *PostgresJobQueue) resolveRefusedEnqueue(ctx context.Context, trackID ids.TrackId, kind ports.JobKind, delay time.Duration) (bool, error) {
	var heldKind, status string
	var leaseIsLive bool
	err := q.pool.QueryRow(ctx, jobStateForRefusedEnqueueSQL, trackID.UUID()).Scan(&heldKind, &status, &leaseIsLive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("enqueue acquisition job: track %s not found", trackID)
	}
	if err != nil {
		return false, fmt.Errorf("enqueue acquisition job: %w", err)
	}
	if kind == ports.JobKindAcquire && status == "ready" {
		return false, nil
	}
	if leaseIsLive {
		return false, conflictUnlessSameKind(ports.JobKind(heldKind), kind)
	}
	applied, err := q.applyEnqueue(ctx, trackID, kind, delay)
	if err != nil || applied {
		return applied, err
	}
	return false, conflictUnlessSameKind(ports.JobKind(heldKind), kind)
}

func conflictUnlessSameKind(held, requested ports.JobKind) error {
	if held != requested {
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

func (q *PostgresJobQueue) PendingDepth(ctx context.Context) (int, time.Duration, error) {
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
	acquisition_started_at = now(),
	version = version + 1
FROM candidate
WHERE tracks.id = candidate.id
RETURNING tracks.id, tracks.user_id, tracks.acquisition_job_kind, tracks.acquisition_attempts, tracks.acquisition_run_attempts,
	GREATEST(EXTRACT(EPOCH FROM now() - tracks.acquisition_available_at), 0)::float8,
	tracks.acquisition_correlation_id`

func (q *PostgresJobQueue) Claim(ctx context.Context, lease time.Duration) (ports.Job, error) {
	return q.claim(ctx, claimJobSQL, lease)
}

func (q *PostgresJobQueue) ClaimUpgrade(ctx context.Context, lease time.Duration) (ports.Job, error) {
	return q.claim(ctx, claimUpgradeJobSQL, lease)
}

func (q *PostgresJobQueue) claim(ctx context.Context, sql string, lease time.Duration) (ports.Job, error) {
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
		trackID  ids.TrackId
		userID   ids.UserId
		kind     string
		attempts int
		run      int
		waited   float64
		corrID   *string
	)
	if err := scanTrackClaim(row, &trackID, &userID, &kind, &attempts, &run, &waited, &corrID); err != nil {
		return ports.Job{}, err
	}
	return ports.Job{
		TrackID: trackID, UserID: userID, Kind: ports.JobKind(kind), Run: run, ClaimNumber: ports.ClaimNumber(attempts),
		Waited: time.Duration(waited * float64(time.Second)), CorrelationID: storedCorrelationID(corrID),
	}, nil
}

func storedCorrelationID(stored *string) string {
	if stored == nil {
		return ""
	}
	return *stored
}

func scanTrackClaim(row pgx.Row, trackID *ids.TrackId, userID *ids.UserId, kind *string, attempts, run *int, waited *float64, corrID **string) error {
	var rawTrackID, rawUserID uuid.UUID
	if err := row.Scan(&rawTrackID, &rawUserID, kind, attempts, run, waited, corrID); err != nil {
		return fmt.Errorf("scan track claim: %w", err)
	}
	*trackID = ids.TrackIdFromUUID(rawTrackID)
	*userID = ids.NewUserId(rawUserID)
	return nil
}

func (q *PostgresJobQueue) execFenced(ctx context.Context, trackID ids.TrackId, op, sql string, args ...any) error {
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

const verifyLeaseSQL = `
SELECT EXISTS (
	SELECT 1 FROM tracks
	WHERE id = $1 AND acquisition_attempts = $2 AND acquisition_available_at IS NOT NULL AND acquisition_lease_until >= now()
)`

func (q *PostgresJobQueue) VerifyLease(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var held bool
	if err := q.pool.QueryRow(ctx, verifyLeaseSQL, trackID.UUID(), int(fence)).Scan(&held); err != nil {
		return fmt.Errorf("verify acquisition lease: %w", err)
	}
	if !held {
		return fmt.Errorf("verify acquisition lease for track %s: %w", trackID, acqdomain.ErrLeaseLost)
	}
	return nil
}

func (q *PostgresJobQueue) Heartbeat(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, lease time.Duration) error {
	return q.execFenced(ctx, trackID, "heartbeat", heartbeatJobSQL, trackID.UUID(), int(fence), lease.Seconds())
}

const releaseJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL, acquisition_available_at = now() + make_interval(secs => $3)
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PostgresJobQueue) Release(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, delay time.Duration) error {
	return q.execFenced(ctx, trackID, "release", releaseJobSQL, trackID.UUID(), int(fence), delay.Seconds())
}

const settleJobSQL = `
UPDATE tracks
SET acquisition_lease_until = NULL,
	acquisition_available_at = CASE
		WHEN acquisition_status = 'pending' THEN COALESCE(acquisition_available_at, now())
	END,
	acquisition_job_kind = CASE
		WHEN acquisition_status = 'pending' THEN acquisition_job_kind
		ELSE 'acquire'
	END
WHERE id = $1 AND acquisition_attempts = $2`

func (q *PostgresJobQueue) Settle(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	return q.execFenced(ctx, trackID, "settle", settleJobSQL, trackID.UUID(), int(fence))
}

const notifierReconnectDelay = time.Second

type PostgresJobNotifier struct {
	pool  *pgxpool.Pool
	sleep clock.Sleeper
}

var _ ports.JobNotifier = (*PostgresJobNotifier)(nil)

var ErrNotifierSleeperRequired = errors.New("job notifier needs a sleeper")

func NewPostgresJobNotifier(pool *pgxpool.Pool, sleep clock.Sleeper) (*PostgresJobNotifier, error) {
	if sleep == nil {
		return nil, fmt.Errorf("NewPostgresJobNotifier: %w", ErrNotifierSleeperRequired)
	}
	return &PostgresJobNotifier{pool: pool, sleep: sleep}, nil
}

func MustNewPostgresJobNotifier(pool *pgxpool.Pool, sleep clock.Sleeper) *PostgresJobNotifier {
	notifier, err := NewPostgresJobNotifier(pool, sleep)
	if err != nil {
		panic(err)
	}
	return notifier
}

func (n *PostgresJobNotifier) Listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		n.listenOnce(ctx, wake)
		if n.sleep(ctx, notifierReconnectDelay) != nil {
			return
		}
	}
}

func warnNotifierCannotListen(ctx context.Context, err error) {
	if ctx.Err() == nil {
		slog.WarnContext(ctx, "acquisition.notifier_listen_failed", "error", err)
	}
}

func (n *PostgresJobNotifier) listenOnce(ctx context.Context, wake chan<- struct{}) {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		warnNotifierCannotListen(ctx, err)
		return
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+acquisitionJobChannel); err != nil {
		warnNotifierCannotListen(ctx, err)
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
