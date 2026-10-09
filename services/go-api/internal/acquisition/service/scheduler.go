package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
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

const outcomeRecordTimeout = 2 * time.Second

type acquirer interface {
	Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
	ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
	RefuseQueued(ctx context.Context, userId shared.UserId, trackId domain.TrackId)
}

type BackgroundAcquisitionScheduler struct {
	svc      acquirer
	events   events.Publisher
	queue    ports.JobQueue
	notifier ports.JobNotifier

	pending   *pendingTracker
	jobsWG    sync.WaitGroup
	runnersWG sync.WaitGroup

	corrIDs sync.Map

	sem chan struct{}

	cancel  context.CancelFunc
	baseCtx context.Context

	closed    atomic.Bool
	paused    atomic.Bool
	stop      chan struct{}
	closeOnce sync.Once
	wake      chan struct{}

	admitMu sync.RWMutex

	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	pollInterval      time.Duration
	drainBudget       time.Duration

	inflightCount atomic.Int64
	rejected      atomic.Uint64

	verification        ports.AcquisitionVerification
	skipCount           func() uint64
	fingerprintVerified func() bool
	log                 *jobLog
	now                 func() time.Time
	outcomes            ports.OutcomeRecorder
	outcomeWG           sync.WaitGroup
}

func NewBackgroundAcquisitionScheduler(
	svc acquirer,
	wg *sync.WaitGroup,
	sem chan struct{},
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &BackgroundAcquisitionScheduler{
		svc:               svc,
		pending:           newPendingTracker(wg),
		sem:               sem,
		cancel:            cancel,
		baseCtx:           ctx,
		stop:              make(chan struct{}),
		wake:              make(chan struct{}, 1),
		log:               newJobLog(),
		now:               time.Now,
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
		s.queue = newMemJobQueueWithClock(s.wake, s.now)
	}
	if s.notifier != nil {
		s.runnersWG.Add(1)
		go func() {
			defer s.runnersWG.Done()
			s.notifier.Listen(s.baseCtx, s.wake)
		}()
	}

	workers := cap(sem)
	if workers < 1 {
		workers = 1
	}
	for range workers {
		s.runnersWG.Add(1)
		go s.runWorker()
	}
	return s
}

func (s *BackgroundAcquisitionScheduler) verificationStatus() ports.AcquisitionVerification {
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

func (s *BackgroundAcquisitionScheduler) runWorker() {
	defer s.runnersWG.Done()
	for {
		if s.closed.Load() {
			return
		}
		if s.paused.Load() {
			if !s.idleWait() {
				return
			}
			continue
		}
		job, ok := s.claimJob()
		if !ok {
			if !s.idleWait() {
				return
			}
			continue
		}
		s.runClaimedJob(job)
	}
}

func (s *BackgroundAcquisitionScheduler) claimJob() (ports.Job, bool) {
	s.admitMu.RLock()
	defer s.admitMu.RUnlock()
	if s.closed.Load() {
		return ports.Job{}, false
	}
	job, ok := s.claimNext()
	if ok {
		s.jobsWG.Add(1)
	}
	return job, ok
}

func (s *BackgroundAcquisitionScheduler) claimNext() (ports.Job, bool) {
	job, err := s.queue.Claim(s.baseCtx, s.leaseDuration)
	if errors.Is(err, ports.ErrNoJobAvailable) {
		return ports.Job{}, false
	}
	if err != nil {
		slog.ErrorContext(s.baseCtx, "acquisition.claim_failed", "error", err)
		return ports.Job{}, false
	}
	return job, true
}

func (s *BackgroundAcquisitionScheduler) idleWait() bool {
	timer := time.NewTimer(s.pollInterval)
	defer timer.Stop()
	select {
	case <-s.wake:
		return true
	case <-timer.C:
		return true
	case <-s.stop:
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) runClaimedJob(job ports.Job) {
	defer s.jobsWG.Done()

	key := job.TrackID.String()
	run := s.acquisitionRunFor(job.Kind)

	jobCtx, cancelJob := s.jobContextFor(key, job.Run)
	defer cancelJob()

	heartbeatDone := make(chan struct{})
	go s.heartbeatLoop(jobCtx, job, cancelJob, heartbeatDone)

	reportedCtx := s.startClaimedJob(jobCtx, key, job.UserID)
	jobErr := s.execClaimedJob(reportedCtx, key, run, job)
	s.stopClaimedJob(cancelJob, heartbeatDone)

	s.finishJob(job, jobErr)
}

func (s *BackgroundAcquisitionScheduler) startClaimedJob(jobCtx context.Context, key string, userId shared.UserId) context.Context {
	s.inflightCount.Add(1)
	s.log.register(key, "")
	s.log.markRunning(key)
	return withJobReporter(jobCtx, schedulerJobReporter{
		ctx: jobCtx, log: s.log, events: s.events, trackID: key, userId: userId,
	})
}

func (s *BackgroundAcquisitionScheduler) stopClaimedJob(cancelJob context.CancelFunc, heartbeatDone <-chan struct{}) {
	cancelJob()
	<-heartbeatDone
	s.inflightCount.Add(-1)
}

func (s *BackgroundAcquisitionScheduler) acquisitionRunFor(kind ports.JobKind) acquisitionRun {
	if kind == ports.JobKindReplace {
		return s.svc.ExecuteReplace
	}
	return s.svc.Execute
}

func (s *BackgroundAcquisitionScheduler) jobContextFor(key string, attempt int) (context.Context, context.CancelFunc) {
	jobCtx, cancelJob := context.WithCancel(s.baseCtx)
	jobCtx = withJobAttempt(withSchedulerOwnedJobContext(jobCtx), attempt)
	if corrID, ok := s.corrIDs.LoadAndDelete(key); ok {
		jobCtx = logging.WithCorrelationID(jobCtx, corrID.(string))
	}
	return jobCtx, cancelJob
}

func (s *BackgroundAcquisitionScheduler) execClaimedJob(ctx context.Context, key string, run acquisitionRun, job ports.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logJobPanic(ctx, key, r)
			err = errJobPanicked
		}
	}()
	err = run(ctx, job.UserID, job.TrackID)
	s.completeClaimedJob(ctx, key, err)
	return err
}

func (s *BackgroundAcquisitionScheduler) completeClaimedJob(ctx context.Context, key string, err error) {
	if err != nil && s.baseCtx.Err() != nil {
		record := s.log.complete(key, ports.JobCancelled, "shutdown")
		s.recordOutcome(ctx, record)
		slog.WarnContext(ctx, "background acquisition cancelled for shutdown", "track_id", key)
		return
	}
	if err != nil {
		reason := logSafeError(err)
		record := s.log.complete(key, ports.JobFailed, reason)
		s.recordOutcome(ctx, record)
		slog.ErrorContext(ctx, "background acquisition failed", "track_id", key, "error", reason)
		return
	}
	record := s.log.complete(key, ports.JobSucceeded, "")
	s.recordOutcome(ctx, record)
}

func (s *BackgroundAcquisitionScheduler) recordOutcome(ctx context.Context, record ports.JobRecord) {
	if s.outcomes == nil || record.TrackID == "" {
		return
	}
	s.outcomeWG.Add(1)
	go func() {
		defer s.outcomeWG.Done()
		recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeRecordTimeout)
		defer cancel()
		err := s.outcomes.Record(recCtx, ports.AcquisitionOutcome{
			TrackID:   record.TrackID,
			Outcome:   record.State,
			Reason:    record.Reason,
			ElapsedMs: record.ElapsedMs,
		})
		if err != nil {
			slog.WarnContext(ctx, "acquisition.outcome_record_failed",
				"track_id", record.TrackID, "error", logSafeError(err))
		}
	}()
}

var errJobPanicked = errors.New("acquisition job panicked")

type acquisitionRun func(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error

func (s *BackgroundAcquisitionScheduler) finishJob(job ports.Job, jobErr error) {
	defer s.pending.untrack(job.TrackID.String())

	if s.baseCtx.Err() != nil {
		err := s.queue.Release(context.Background(), job.TrackID, job.Fence, 0)
		s.logQueueOutcome("release", job.TrackID, err)
		return
	}
	if errors.Is(jobErr, errJobPanicked) {
		s.releasePanickedJob(job)
		return
	}
	if errors.Is(jobErr, ErrAcquisitionRetryable) {
		err := s.queue.Release(context.Background(), job.TrackID, job.Fence, retryBackoff(job.Run))
		s.logQueueOutcome("release", job.TrackID, err)
		return
	}
	err := s.queue.Settle(context.Background(), job.TrackID, job.Fence)
	s.logQueueOutcome("settle", job.TrackID, err)
}

func (s *BackgroundAcquisitionScheduler) releasePanickedJob(job ports.Job) {
	if job.Run >= maxAcquisitionAttempts {
		s.refuseQueuedRecovering(job)
	}
	err := s.queue.Release(context.Background(), job.TrackID, job.Fence, retryBackoff(job.Run))
	s.logQueueOutcome("release", job.TrackID, err)
}

func (s *BackgroundAcquisitionScheduler) refuseQueuedRecovering(job ports.Job) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(s.baseCtx, "acquisition_panic_fail_track", "track_id", job.TrackID.String(), "panic", r)
		}
	}()
	s.svc.RefuseQueued(context.Background(), job.UserID, job.TrackID)
}

func (s *BackgroundAcquisitionScheduler) logQueueOutcome(op string, trackID domain.TrackId, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ports.ErrLeaseLost) {
		slog.WarnContext(s.baseCtx, "acquisition."+op+"_lease_lost", "track_id", trackID.String())
		return
	}
	slog.ErrorContext(s.baseCtx, "acquisition."+op+"_failed", "track_id", trackID.String(), "error", err)
}

func (s *BackgroundAcquisitionScheduler) logJobPanic(jobCtx context.Context, key string, r any) {
	record := s.log.complete(key, ports.JobFailed, "panic")
	s.recordOutcome(jobCtx, record)
	slog.ErrorContext(jobCtx, "acquisition_panic",
		"track_id", key,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}
