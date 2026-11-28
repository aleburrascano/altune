package persistence

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const acquisitionJobChannel = "acquisition_jobs"

const claimRequeueRetryDelay = 5 * time.Second

type PostgresJobQueue struct {
	pool     *pgxpool.Pool
	requeuer ports.QueueTracks
}

var (
	_ ports.DownloadQueue    = (*PostgresJobQueue)(nil)
	_ ports.QueueDepthReader = (*PostgresJobQueue)(nil)
	_ ports.UpgradeJobQueue  = (*PostgresJobQueue)(nil)
)

var ErrJobQueueRequeuerRequired = errors.New("job queue needs a track requeuer")

func NewPostgresJobQueue(pool *pgxpool.Pool, requeuer ports.QueueTracks) (*PostgresJobQueue, error) {
	if requeuer == nil {
		return nil, fmt.Errorf("NewPostgresJobQueue: %w", ErrJobQueueRequeuerRequired)
	}
	return &PostgresJobQueue{pool: pool, requeuer: requeuer}, nil
}

func MustNewPostgresJobQueue(pool *pgxpool.Pool, requeuer ports.QueueTracks) *PostgresJobQueue {
	queue, err := NewPostgresJobQueue(pool, requeuer)
	if err != nil {
		panic(err)
	}
	return queue
}

const ensureJobRowSQL = `INSERT INTO acquisition_jobs (track_id) SELECT id FROM tracks WHERE id = $1 FOR UPDATE ON CONFLICT (track_id) DO NOTHING`

const (
	claimableParam = 2
	retryableParam = 3
)

func claimableJobSQL(claimable, retryable int) string {
	return fmt.Sprintf(`(t.acquisition_status = ANY($%d) OR j.job_kind = 'replace' OR (t.acquisition_status = ANY($%d) AND j.needs_pending))`, claimable, retryable)
}

func (q *PostgresJobQueue) claimableArgs() []any {
	return []any{domain.StatusWires(q.requeuer.Claimable()), domain.StatusWires(q.requeuer.Retryable())}
}

const clearNeedsPendingSQL = `
UPDATE acquisition_jobs j SET needs_pending = false
FROM (SELECT id FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id AND j.needs_pending`

const enqueueJobSQL = `
UPDATE acquisition_jobs j
SET job_kind = $2,
	available_at = now() + make_interval(secs => $3),
	lease_until = CASE
		WHEN j.job_kind = 'upgrade' AND $2 <> 'upgrade' THEN NULL
		WHEN (t.acquisition_status = ANY($5) OR j.available_at IS NOT NULL)
			AND j.lease_until >= now() THEN j.lease_until
	END,
	attempts = j.attempts
		+ CASE
			WHEN j.job_kind = 'upgrade' AND $2 <> 'upgrade' THEN 1
			WHEN j.lease_until < now() THEN 1
			ELSE 0
		END,
	run_attempts = 0,
	correlation_id = NULLIF($4, ''),
	needs_pending = (t.acquisition_status = ANY($6))
FROM (SELECT id, acquisition_status FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id
	AND NOT ($2 = 'acquire' AND t.acquisition_status = ANY($7))
	AND (
		$2 <> 'upgrade'
		OR (t.acquisition_status = ANY($7) AND (j.available_at IS NULL OR j.job_kind = 'upgrade'))
	)
	AND (
		(j.job_kind = 'upgrade' AND $2 <> 'upgrade')
		OR (NOT t.acquisition_status = ANY($5) AND j.available_at IS NULL)
		OR j.lease_until IS NULL
		OR j.lease_until < now()
		OR j.job_kind = $2
	)`

const jobStateForRefusedEnqueueSQL = `
SELECT j.job_kind, t.acquisition_status, COALESCE(j.lease_until >= now(), false)
FROM tracks t JOIN acquisition_jobs j ON j.track_id = t.id
WHERE t.id = $1`

func (q *PostgresJobQueue) Enqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	applied, err := q.applyOrResolveEnqueue(ctx, trackID, kind, delay)
	if err != nil || !applied {
		return err
	}
	q.requeueTrack(ctx, trackID)
	if _, err := q.pool.Exec(ctx, "NOTIFY "+acquisitionJobChannel); err != nil {
		slog.WarnContext(ctx, "acquisition.enqueue_notify_failed",
			logging.KeyTrackID, trackID.String(), logging.KeyError, err)
	}
	return nil
}

func (q *PostgresJobQueue) applyOrResolveEnqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) (bool, error) {
	applied, err := q.applyEnqueue(ctx, trackID, kind, delay)
	if err != nil || applied {
		return applied, err
	}
	return q.resolveRefusedEnqueue(ctx, trackID, kind, delay)
}

func (q *PostgresJobQueue) requeueTrack(ctx context.Context, trackID ids.TrackId) {
	if err := q.requeuer.Requeue(ctx, trackID); err != nil {
		slog.WarnContext(ctx, "acquisition.enqueue_requeue_failed",
			logging.KeyTrackID, trackID.String(), logging.KeyError, err)
		return
	}
	q.clearNeedsPending(ctx, trackID)
}

func (q *PostgresJobQueue) applyEnqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) (bool, error) {
	if _, err := q.pool.Exec(ctx, ensureJobRowSQL, trackID.UUID()); err != nil {
		return false, fmt.Errorf("enqueue acquisition job: %w", err)
	}
	tag, err := q.pool.Exec(ctx, enqueueJobSQL, trackID.UUID(), string(kind), delay.Seconds(), logging.CorrelationIDFromContext(ctx),
		domain.StatusWires(q.requeuer.Claimable()), domain.StatusWires(q.requeuer.Retryable()), domain.StatusWires(q.requeuer.Upgradable()))
	if err != nil {
		return false, fmt.Errorf("enqueue acquisition job: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (q *PostgresJobQueue) resolveRefusedEnqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) (bool, error) {
	var heldKind, status string
	var leaseIsLive bool
	err := q.pool.QueryRow(ctx, jobStateForRefusedEnqueueSQL, trackID.UUID()).Scan(&heldKind, &status, &leaseIsLive)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("enqueue acquisition job: track %s not found", trackID)
	}
	if err != nil {
		return false, fmt.Errorf("enqueue acquisition job: %w", err)
	}
	if kind == domain.JobKindAcquire && slices.Contains(domain.StatusWires(q.requeuer.Upgradable()), status) {
		return false, nil
	}
	if leaseIsLive {
		return false, conflictUnlessSameKind(domain.JobKind(heldKind), kind)
	}
	applied, err := q.applyEnqueue(ctx, trackID, kind, delay)
	if err != nil || applied {
		return applied, err
	}
	return false, conflictUnlessSameKind(domain.JobKind(heldKind), kind)
}

func conflictUnlessSameKind(held, requested domain.JobKind) error {
	if held != requested {
		return domain.ErrJobKindConflict
	}
	return nil
}

var pendingDepthSQL = `
SELECT count(*),
	COALESCE(EXTRACT(EPOCH FROM now() - min(j.available_at) FILTER (WHERE j.available_at <= now())), 0)::float8
FROM acquisition_jobs j JOIN tracks t ON t.id = j.track_id
WHERE j.available_at IS NOT NULL
	AND ` + claimableJobSQL(1, 2) + `
	AND (j.lease_until IS NULL OR j.lease_until < now())`

func (q *PostgresJobQueue) PendingDepth(ctx context.Context) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var (
		pending    int
		oldestSecs float64
	)
	if err := q.pool.QueryRow(ctx, pendingDepthSQL, q.claimableArgs()...).Scan(&pending, &oldestSecs); err != nil {
		return 0, 0, fmt.Errorf("read acquisition pending depth: %w", err)
	}
	return pending, time.Duration(oldestSecs * float64(time.Second)), nil
}

var claimJobSQL = `
WITH candidate AS (
	SELECT j.track_id AS id
	FROM acquisition_jobs j JOIN tracks t ON t.id = j.track_id
	WHERE j.available_at <= now()
		AND j.job_kind <> 'upgrade'
		AND ` + claimableJobSQL(claimableParam, retryableParam) + `
		AND (j.lease_until IS NULL OR j.lease_until < now())
	ORDER BY (
			SELECT count(*) FROM acquisition_jobs u JOIN tracks ut ON ut.id = u.track_id
			WHERE ut.user_id = t.user_id
				AND u.available_at IS NOT NULL
				AND u.lease_until >= now()
		), j.available_at
	FOR UPDATE OF t SKIP LOCKED
	LIMIT 1
)
` + leaseClaimedJobSQL

var claimUpgradeJobSQL = `
WITH candidate AS (
	SELECT j.track_id AS id
	FROM acquisition_jobs j JOIN tracks t ON t.id = j.track_id
	WHERE j.available_at <= now()
		AND j.job_kind = 'upgrade'
		AND (j.lease_until IS NULL OR j.lease_until < now())
		AND NOT EXISTS (
			SELECT 1 FROM acquisition_jobs u JOIN tracks ut ON ut.id = u.track_id
			WHERE u.available_at <= now()
				AND u.job_kind <> 'upgrade'
				AND (ut.acquisition_status = ANY($2) OR u.job_kind = 'replace' OR (ut.acquisition_status = ANY($3) AND u.needs_pending))
				AND (u.lease_until IS NULL OR u.lease_until < now())
		)
	ORDER BY j.available_at
	FOR UPDATE OF t SKIP LOCKED
	LIMIT 1
)
` + leaseClaimedJobSQL

const leaseClaimedJobSQL = `UPDATE acquisition_jobs j
SET lease_until = now() + make_interval(secs => $1),
	attempts = j.attempts + 1,
	run_attempts = j.run_attempts + 1
FROM candidate
WHERE j.track_id = candidate.id
	AND (j.lease_until IS NULL OR j.lease_until < now())
RETURNING j.track_id, (SELECT t.user_id FROM tracks t WHERE t.id = j.track_id), j.job_kind, j.attempts, j.run_attempts,
	GREATEST(EXTRACT(EPOCH FROM now() - j.available_at), 0)::float8,
	j.correlation_id,
	j.needs_pending`

func (q *PostgresJobQueue) Claim(ctx context.Context, lease time.Duration) (domain.Job, error) {
	return q.claim(ctx, claimJobSQL, lease)
}

func (q *PostgresJobQueue) ClaimUpgrade(ctx context.Context, lease time.Duration) (domain.Job, error) {
	return q.claim(ctx, claimUpgradeJobSQL, lease)
}

func (q *PostgresJobQueue) claim(ctx context.Context, sql string, lease time.Duration) (domain.Job, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	job, needsPending, err := scanClaimedJob(q.pool.QueryRow(ctx, sql, append([]any{lease.Seconds()}, q.claimableArgs()...)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNoJobAvailable
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("claim acquisition job: %w", err)
	}
	if !needsPending {
		return job, nil
	}
	if err := q.requeuer.Requeue(ctx, job.TrackID); err != nil {
		q.giveBackClaim(ctx, job)
		return domain.Job{}, fmt.Errorf("mark the claimed track pending: %w", err)
	}
	q.clearNeedsPending(ctx, job.TrackID)
	return job, nil
}

func (q *PostgresJobQueue) giveBackClaim(ctx context.Context, job domain.Job) {
	if err := q.releaseUnstarted(ctx, job); err != nil {
		slog.WarnContext(ctx, "acquisition.claim_release_failed",
			logging.KeyTrackID, job.TrackID.String(), logging.KeyError, err)
	}
}

func scanClaimedJob(row pgx.Row) (domain.Job, bool, error) {
	var (
		trackID  ids.TrackId
		userID   ids.UserId
		kind     string
		attempts int
		run      int
		waited   float64
		corrID   *string
	)
	var needsPending bool
	if err := scanTrackClaim(row, &trackID, &userID, &kind, &attempts, &run, &waited, &corrID, &needsPending); err != nil {
		return domain.Job{}, false, err
	}
	return domain.Job{
		TrackID: trackID, UserID: userID, Kind: domain.JobKind(kind), Run: run, ClaimNumber: domain.ClaimNumber(attempts),
		Waited: time.Duration(waited * float64(time.Second)), CorrelationID: storedCorrelationID(corrID),
	}, needsPending, nil
}

func storedCorrelationID(stored *string) string {
	if stored == nil {
		return ""
	}
	return *stored
}

func scanTrackClaim(row pgx.Row, trackID *ids.TrackId, userID *ids.UserId, kind *string, attempts, run *int, waited *float64, corrID **string, needsPending *bool) error {
	var rawTrackID, rawUserID uuid.UUID
	if err := row.Scan(&rawTrackID, &rawUserID, kind, attempts, run, waited, corrID, needsPending); err != nil {
		return fmt.Errorf("scan track claim: %w", err)
	}
	*trackID = ids.TrackIdFromUUID(rawTrackID)
	*userID = ids.NewUserId(rawUserID)
	return nil
}

func (q *PostgresJobQueue) clearNeedsPending(ctx context.Context, trackID ids.TrackId) {
	if _, err := q.pool.Exec(ctx, clearNeedsPendingSQL, trackID.UUID()); err != nil {
		slog.WarnContext(ctx, "acquisition.clear_needs_pending_failed",
			logging.KeyTrackID, trackID.String(), logging.KeyError, err)
	}
}

func (q *PostgresJobQueue) execFenced(ctx context.Context, trackID ids.TrackId, op, sql string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := q.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("%s acquisition job: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s acquisition job for track %s: %w", op, trackID, domain.ErrLeaseLost)
	}
	return nil
}

const heartbeatJobSQL = `
UPDATE acquisition_jobs j
SET lease_until = now() + make_interval(secs => $3)
FROM (SELECT id FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id AND j.attempts = $2 AND j.available_at IS NOT NULL AND j.lease_until >= now()`

const verifyLeaseSQL = `
SELECT EXISTS (
	SELECT 1 FROM acquisition_jobs
	WHERE track_id = $1 AND attempts = $2 AND available_at IS NOT NULL AND lease_until >= now()
)`

func (q *PostgresJobQueue) VerifyLease(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var held bool
	if err := q.pool.QueryRow(ctx, verifyLeaseSQL, trackID.UUID(), int(fence)).Scan(&held); err != nil {
		return fmt.Errorf("verify acquisition lease: %w", err)
	}
	if !held {
		return fmt.Errorf("verify acquisition lease for track %s: %w", trackID, domain.ErrLeaseLost)
	}
	return nil
}

func (q *PostgresJobQueue) Heartbeat(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, lease time.Duration) error {
	return q.execFenced(ctx, trackID, "heartbeat", heartbeatJobSQL, trackID.UUID(), int(fence), lease.Seconds())
}

const releaseJobSQL = `
UPDATE acquisition_jobs j
SET lease_until = NULL, available_at = now() + make_interval(secs => $3)
FROM (SELECT id FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id AND j.attempts = $2`

func (q *PostgresJobQueue) Release(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, delay time.Duration) error {
	return q.execFenced(ctx, trackID, "release", releaseJobSQL, trackID.UUID(), int(fence), delay.Seconds())
}

const releaseUnstartedJobSQL = `
UPDATE acquisition_jobs j
SET lease_until = NULL, available_at = now() + make_interval(secs => $3), run_attempts = GREATEST(j.run_attempts - 1, 0)
FROM (SELECT id FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id AND j.attempts = $2`

func (q *PostgresJobQueue) releaseUnstarted(ctx context.Context, job domain.Job) error {
	return q.execFenced(ctx, job.TrackID, "release", releaseUnstartedJobSQL, job.TrackID.UUID(), int(job.ClaimNumber), claimRequeueRetryDelay.Seconds())
}

const settleJobSQL = `
UPDATE acquisition_jobs j
SET lease_until = NULL,
	needs_pending = false,
	available_at = CASE
		WHEN t.acquisition_status = ANY($3) THEN COALESCE(j.available_at, now())
	END,
	job_kind = CASE
		WHEN t.acquisition_status = ANY($3) THEN j.job_kind
		ELSE 'acquire'
	END
FROM (SELECT id, acquisition_status FROM tracks WHERE id = $1 FOR UPDATE) t
WHERE j.track_id = t.id AND j.attempts = $2`

func (q *PostgresJobQueue) Settle(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error {
	return q.execFenced(ctx, trackID, "settle", settleJobSQL, trackID.UUID(), int(fence), domain.StatusWires(q.requeuer.Claimable()))
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
		slog.WarnContext(ctx, "acquisition.notifier_listen_failed", logging.KeyError, err)
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
