package service

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLeaseDuration     = 2 * time.Minute
	defaultHeartbeatInterval = 30 * time.Second
	defaultPollInterval      = 5 * time.Second
	defaultDrainBudget       = 60 * time.Second
)

const (
	OutcomeRecordTimeout = 2 * time.Second
	queueCallBudget      = 10 * time.Second
)

type acquirer interface {
	Execute(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
	ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
	RefuseQueued(ctx context.Context, userId ids.UserId, trackId ids.TrackId)
}

type BackgroundAcquisitionScheduler struct {
	svc      acquirer
	events   events.Publisher
	queue    ports.DownloadQueue
	notifier ports.JobNotifier

	pending *pendingTracker
	jobs    background.Tracker
	runners background.Tracker

	workers int

	cancel  context.CancelFunc
	baseCtx context.Context

	closed    atomic.Bool
	paused    atomic.Bool
	stop      chan struct{}
	closeOnce sync.Once
	wake      chan struct{}
	lanes     laneWakes

	admitMu sync.RWMutex

	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	pollInterval      time.Duration
	drainBudget       time.Duration

	inflightCount atomic.Int64
	rejected      atomic.Uint64

	verification        ports.Verification
	skipCount           func() uint64
	fingerprintVerified func() bool
	log                 *jobLog
	now                 func() time.Time
	outcomes            ports.OutcomeRecorder
	outcomeRecords      background.Tracker

	upgrader       upgradeRunner
	upgradeWorkers int
}

func NewBackgroundAcquisitionScheduler(
	svc acquirer,
	workers int,
	now clock.Func,
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	requirePort("NewBackgroundAcquisitionScheduler", "clock", now == nil)
	ctx, cancel := context.WithCancel(context.Background())
	s := &BackgroundAcquisitionScheduler{
		svc:               svc,
		pending:           newPendingTracker(),
		workers:           max(workers, 1),
		cancel:            cancel,
		baseCtx:           ctx,
		stop:              make(chan struct{}),
		wake:              make(chan struct{}, 1),
		lanes:             newLaneWakes(),
		log:               newJobLog(now),
		now:               now,
		events:            events.NoopPublisher(),
		leaseDuration:     defaultLeaseDuration,
		heartbeatInterval: defaultHeartbeatInterval,
		pollInterval:      defaultPollInterval,
		drainBudget:       defaultDrainBudget,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.queue == nil {
		s.queue = inmemoryqueue.New(s.wake, s.now)
	}
	if s.notifier != nil {
		s.runners.Track(func() { s.notifier.Listen(s.baseCtx, s.wake) })
	}
	s.runners.Track(s.fanOutWakes)

	for range s.workers {
		s.runners.Track(func() { s.runWorker(s.claimJob, s.lanes.acquire) })
	}
	s.startUpgradeWorkers()
	return s
}

func (s *BackgroundAcquisitionScheduler) startUpgradeWorkers() {
	if s.upgrader == nil {
		return
	}
	queue, ok := s.queue.(ports.UpgradeJobQueue)
	if !ok {
		slog.ErrorContext(s.baseCtx, "acquisition.upgrade_lane_unavailable", "reason", "the job queue cannot claim upgrade jobs")
		return
	}
	claim := func() (<-chan struct{}, bool) { return s.claimFrom(queue.ClaimUpgrade) }
	for range s.upgradeWorkers {
		s.runners.Track(func() { s.runWorker(claim, s.lanes.upgrade) })
	}
}

func (s *BackgroundAcquisitionScheduler) withLeaseFence(ctx context.Context, job ports.Job) context.Context {
	verifier, ok := s.queue.(ports.UpgradeJobQueue)
	if !ok || job.Kind != ports.JobKindUpgrade {
		return ctx
	}
	return withCommitCheck(ctx, func(ctx context.Context) error {
		return verifier.VerifyLease(ctx, job.TrackID, job.ClaimNumber)
	})
}

func (s *BackgroundAcquisitionScheduler) eventsFor(kind ports.JobKind) events.Publisher {
	if kind == ports.JobKindUpgrade {
		return events.NoopPublisher()
	}
	return s.events
}

func (s *BackgroundAcquisitionScheduler) verificationStatus() ports.Verification {
	v := s.verification
	v.FingerprintVerified = s.fingerprintVerified != nil && s.fingerprintVerified()
	return v
}

func (s *BackgroundAcquisitionScheduler) verifySkipped() uint64 {
	if s.skipCount == nil {
		return 0
	}
	return s.skipCount()
}

func (s *BackgroundAcquisitionScheduler) Pause() { s.paused.Store(true) }

func (s *BackgroundAcquisitionScheduler) Resume() {
	s.paused.Store(false)
	notifyWake(s.wake)
}

func (s *BackgroundAcquisitionScheduler) runWorker(claim func() (<-chan struct{}, bool), wake chan struct{}) {
	for {
		if s.closed.Load() {
			return
		}
		done, ok := s.nextJob(claim)
		if !ok {
			if !s.idleWait(wake) {
				return
			}
			continue
		}
		notifyWake(wake)
		<-done
	}
}

func (s *BackgroundAcquisitionScheduler) nextJob(claim func() (<-chan struct{}, bool)) (<-chan struct{}, bool) {
	if s.paused.Load() {
		return nil, false
	}
	return claim()
}

func (s *BackgroundAcquisitionScheduler) claimJob() (<-chan struct{}, bool) {
	return s.claimFrom(s.queue.Claim)
}

type claimFunc func(ctx context.Context, lease time.Duration) (ports.Job, error)

func (s *BackgroundAcquisitionScheduler) claimFrom(claim claimFunc) (<-chan struct{}, bool) {
	s.admitMu.RLock()
	defer s.admitMu.RUnlock()
	if s.closed.Load() {
		return nil, false
	}
	job, ok := s.claimNext(claim)
	if !ok {
		return nil, false
	}
	done := make(chan struct{})
	s.jobs.Track(func() {
		defer close(done)
		s.runClaimedJob(job)
	})
	return done, true
}

func (s *BackgroundAcquisitionScheduler) claimNext(claim claimFunc) (ports.Job, bool) {
	job, err := claim(s.baseCtx, s.leaseDuration)
	if errors.Is(err, acqdomain.ErrNoJobAvailable) {
		return ports.Job{}, false
	}
	if err != nil {
		slog.ErrorContext(s.baseCtx, "acquisition.claim_failed", "error", err)
		return ports.Job{}, false
	}
	return job, true
}

type laneWakes struct {
	acquire chan struct{}
	upgrade chan struct{}
}

func newLaneWakes() laneWakes {
	return laneWakes{acquire: make(chan struct{}, 1), upgrade: make(chan struct{}, 1)}
}

func (s *BackgroundAcquisitionScheduler) fanOutWakes() {
	for {
		select {
		case <-s.wake:
			notifyWake(s.lanes.acquire)
			notifyWake(s.lanes.upgrade)
		case <-s.stop:
			return
		}
	}
}

func notifyWake(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *BackgroundAcquisitionScheduler) idleWait(wake <-chan struct{}) bool {
	timer := time.NewTimer(s.pollInterval)
	defer timer.Stop()
	select {
	case <-wake:
		return true
	case <-timer.C:
		return true
	case <-s.stop:
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) runClaimedJob(job ports.Job) {
	key := job.TrackID.String()
	run := s.acquisitionRunFor(job.Kind)

	jobCtx, cancelJob := s.jobContextFor(job)
	defer cancelJob()
	jobCtx = s.withLeaseFence(jobCtx, job)

	heartbeatDone := make(chan struct{})
	go s.heartbeatLoop(jobCtx, job, cancelJob, heartbeatDone)

	reportedCtx := s.startClaimedJob(jobCtx, key, job)
	jobErr := s.execClaimedJob(reportedCtx, key, run, job)
	s.stopClaimedJob(cancelJob, heartbeatDone)

	s.finishJob(finishContextOf(jobCtx), job, jobErr)
}

func finishContextOf(jobCtx context.Context) context.Context {
	return logging.WithCorrelationID(context.Background(), logging.CorrelationIDFromContext(jobCtx))
}

func (s *BackgroundAcquisitionScheduler) releaseWithin(ctx context.Context, job ports.Job, delay time.Duration) error {
	queueCtx, cancel := background.Detach(ctx, queueCallBudget)
	defer cancel()
	return s.queue.Release(queueCtx, job.TrackID, job.ClaimNumber, delay)
}

func (s *BackgroundAcquisitionScheduler) settleWithin(ctx context.Context, job ports.Job) error {
	queueCtx, cancel := background.Detach(ctx, queueCallBudget)
	defer cancel()
	return s.queue.Settle(queueCtx, job.TrackID, job.ClaimNumber)
}

func (s *BackgroundAcquisitionScheduler) startClaimedJob(jobCtx context.Context, key string, job ports.Job) context.Context {
	s.inflightCount.Add(1)
	s.log.register(key, "")
	s.log.markRunning(key)
	s.log.update(key, func(r *ports.JobRecord) {
		r.Kind = job.Kind
		r.Run = job.Run
	})
	reporter := schedulerJobReporter{ctx: jobCtx, log: s.log, events: s.eventsFor(job.Kind), trackID: key, userId: job.UserID}
	reporter.timing("queue", job.Waited)
	return withJobReporter(jobCtx, reporter)
}

func (s *BackgroundAcquisitionScheduler) stopClaimedJob(cancelJob context.CancelFunc, heartbeatDone <-chan struct{}) {
	cancelJob()
	<-heartbeatDone
	s.inflightCount.Add(-1)
}

func (s *BackgroundAcquisitionScheduler) acquisitionRunFor(kind ports.JobKind) acquisitionRun {
	switch kind {
	case ports.JobKindReplace:
		return s.svc.ExecuteReplace
	case ports.JobKindUpgrade:
		return s.upgrader.ExecuteUpgrade
	default:
		return s.svc.Execute
	}
}

func (s *BackgroundAcquisitionScheduler) jobContextFor(job ports.Job) (context.Context, context.CancelFunc) {
	jobCtx, cancelJob := context.WithCancel(s.baseCtx)
	jobCtx = withJobFence(withJobAttempt(withSchedulerOwnedJobContext(jobCtx), job.Run), job.ClaimNumber)
	if job.CorrelationID != "" {
		jobCtx = logging.WithCorrelationID(jobCtx, job.CorrelationID)
	}
	return jobCtx, cancelJob
}

func (s *BackgroundAcquisitionScheduler) execClaimedJob(ctx context.Context, key string, run acquisitionRun, job ports.Job) error {
	var err error
	if panicked := background.SafeRun(ctx, "acquisition_panic", func() { err = run(ctx, job.UserID, job.TrackID) }, "track_id", key); panicked != nil {
		err = errJobPanicked
	}
	s.completeClaimedJob(ctx, key, err)
	return err
}

const LeaseLostReason = "lease_lost"

type jobCompletion struct {
	state   ports.JobState
	reason  string
	level   slog.Level
	message string
	attrs   []any
}

func (s *BackgroundAcquisitionScheduler) completeClaimedJob(ctx context.Context, key string, err error) {
	defer background.RecoverContext(ctx, "acquisition.job_completion_panic", "track_id", key)
	done := s.classifyJob(ctx, err)
	record := s.log.complete(key, done.state, done.reason)
	if done.state != ports.JobRetrying {
		s.recordOutcome(ctx, record)
	}
	if done.message != "" {
		slog.Log(ctx, done.level, done.message, append([]any{"track_id", key}, done.attrs...)...)
	}
}

var leaseLost = jobCompletion{state: ports.JobCancelled, reason: LeaseLostReason, level: slog.LevelWarn, message: "background acquisition cancelled after lease loss"}

func (s *BackgroundAcquisitionScheduler) classifyJob(ctx context.Context, err error) jobCompletion {
	switch {
	case errors.Is(err, errJobPanicked):
		return jobCompletion{state: ports.JobFailed, reason: "panic"}
	case err != nil && s.baseCtx.Err() != nil:
		return jobCompletion{state: ports.JobCancelled, reason: "shutdown", level: slog.LevelWarn, message: "background acquisition cancelled for shutdown"}
	case errors.Is(err, replacement.ErrKept):
		return jobCompletion{state: ports.JobKept}
	case errors.Is(err, acqdomain.ErrLeaseLost):
		return leaseLost
	case errors.Is(err, ErrAcquisitionRetryable):
		reason := redact.LogError(err)
		return jobCompletion{state: ports.JobRetrying, reason: reason, level: slog.LevelInfo, message: "background acquisition will retry", attrs: []any{"reason", reason}}
	case err != nil && schedulerCancelledJobContext(ctx):
		return leaseLost
	case err != nil:
		reason := redact.LogError(err)
		return jobCompletion{state: ports.JobFailed, reason: reason, level: slog.LevelError, message: "background acquisition failed", attrs: []any{"error", reason}}
	default:
		return jobCompletion{state: ports.JobSucceeded}
	}
}

func (s *BackgroundAcquisitionScheduler) recordOutcome(ctx context.Context, record ports.JobRecord) {
	if s.outcomes == nil || record.TrackID == "" {
		return
	}
	var sourceHost string
	if record.State == ports.JobSucceeded {
		sourceHost = hostOf(record.ResolvedSource)
	}
	s.outcomeRecords.Track(func() {
		recCtx, cancel := background.Detach(ctx, OutcomeRecordTimeout)
		defer cancel()
		err := s.outcomes.Record(recCtx, ports.AcquisitionOutcome{
			TrackID:    record.TrackID,
			Outcome:    record.State,
			Reason:     record.Reason,
			ElapsedMs:  record.ElapsedMs,
			StageMs:    record.StageMs,
			SourceHost: sourceHost,
			Source:     record.Source,
			Stored:     record.Stored,
			Kind:       record.Kind,
			Run:        record.Run,
			Provenance: record.Provenance,

			Confidence:      record.Confidence,
			SourceKey:       record.SourceKey,
			TargetSeconds:   record.TargetSeconds,
			MeasuredSeconds: record.MeasuredSeconds,
		})
		if err != nil {
			slog.WarnContext(ctx, "acquisition.outcome_record_failed",
				"track_id", record.TrackID, "error", redact.LogError(err))
		}
	})
}

var errJobPanicked = errors.New("acquisition job panicked")

type acquisitionRun func(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error

func (s *BackgroundAcquisitionScheduler) finishJob(ctx context.Context, job ports.Job, jobErr error) {
	defer s.pending.untrack(job.TrackID.String())

	if s.baseCtx.Err() != nil {
		s.logQueueOutcome(ctx, "release", job.TrackID, s.releaseWithin(ctx, job, 0))
		return
	}
	if errors.Is(jobErr, errJobPanicked) {
		s.releasePanickedJob(ctx, job)
		return
	}
	if errors.Is(jobErr, ErrAcquisitionRetryable) {
		s.logQueueOutcome(ctx, "release", job.TrackID, s.releaseWithin(ctx, job, retryBackoff(job.Run)))
		return
	}
	s.logQueueOutcome(ctx, "settle", job.TrackID, s.settleWithin(ctx, job))
}

func (s *BackgroundAcquisitionScheduler) releasePanickedJob(ctx context.Context, job ports.Job) {
	if job.Run >= MaxAcquisitionAttempts {
		s.refuseQueuedRecovering(ctx, job)
		s.logQueueOutcome(ctx, "settle", job.TrackID, s.settleWithin(ctx, job))
		return
	}
	s.logQueueOutcome(ctx, "release", job.TrackID, s.releaseWithin(ctx, job, retryBackoff(job.Run)))
}

func (s *BackgroundAcquisitionScheduler) refuseQueuedRecovering(ctx context.Context, job ports.Job) {
	defer background.RecoverContext(ctx, "acquisition_panic_fail_track", "track_id", job.TrackID.String())
	s.svc.RefuseQueued(withJobFence(ctx, job.ClaimNumber), job.UserID, job.TrackID)
}

func (s *BackgroundAcquisitionScheduler) logQueueOutcome(ctx context.Context, op string, trackID ids.TrackId, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, acqdomain.ErrLeaseLost) {
		slog.WarnContext(ctx, "acquisition."+op+"_lease_lost", "track_id", trackID.String())
		return
	}
	slog.ErrorContext(ctx, "acquisition."+op+"_failed", "track_id", trackID.String(), "error", err)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
