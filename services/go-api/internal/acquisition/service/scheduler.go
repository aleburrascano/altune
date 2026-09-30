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
		s.queue = newMemJobQueue(s.wake)
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

func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if pub != nil {
			s.events = pub
		}
	}
}

func WithJobQueue(q ports.JobQueue) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if q != nil {
			s.queue = q
		}
	}
}

func WithJobNotifier(n ports.JobNotifier) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.notifier = n }
}

func WithDrainBudget(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.drainBudget = d
		}
	}
}

func WithLeaseDuration(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.leaseDuration = d
		}
	}
}

func WithHeartbeatInterval(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.heartbeatInterval = d
		}
	}
}

func WithPollInterval(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.pollInterval = d
		}
	}
}

func WithOutcomeRecorder(r ports.OutcomeRecorder) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.outcomes = r }
}

func WithVerificationStatus(v ports.AcquisitionVerification) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		s.verification = v
		if !v.FullyArmed() {
			slog.Warn("acquisition.verification_degraded",
				"ffprobe", v.Ffprobe, "ffmpeg", v.Ffmpeg, "fpcalc", v.Fpcalc, "yt_dlp", v.YtDlp, "streamrip", v.Streamrip)
			return
		}
		slog.Info("acquisition.verification_armed")
	}
}

func WithVerifySkipCount(count func() uint64) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.skipCount = count }
}

func WithFingerprintVerified(verified func() bool) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.fingerprintVerified = verified }
}

type VerifySkipCounter struct{ n atomic.Uint64 }

func (c *VerifySkipCounter) RecordVerifySkip(string) { c.n.Add(1) }

func (c *VerifySkipCounter) Count() uint64 { return c.n.Load() }

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

func (s *BackgroundAcquisitionScheduler) ScheduleReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindReplace)
}

func (s *BackgroundAcquisitionScheduler) Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, _ string) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindAcquire)
}

func (s *BackgroundAcquisitionScheduler) enqueue(ctx context.Context, userId shared.UserId, trackId domain.TrackId, kind ports.JobKind) error {
	if s.closed.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "schedule_after_shutdown", "track_id", trackId.String())
		return ErrSchedulerShutdown
	}
	if s.paused.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.schedule_while_paused", "track_id", trackId.String())
		return ErrAcquisitionPaused
	}

	key := trackId.String()
	alreadyTracked := s.pending.track(key)
	s.primeEnqueue(ctx, trackId, userId, key)

	if err := s.queue.Enqueue(ctx, trackId, kind, time.Now()); err != nil {
		return s.handleEnqueueError(ctx, key, trackId, kind, alreadyTracked, err)
	}
	return s.finishEnqueue(ctx, trackId, userId, kind)
}

func (s *BackgroundAcquisitionScheduler) primeEnqueue(ctx context.Context, trackId domain.TrackId, userId shared.UserId, key string) {
	if mq, ok := s.queue.(*memJobQueue); ok {
		mq.rememberUser(trackId, userId)
	}
	if corrID := logging.CorrelationIDFromContext(ctx); corrID != "" {
		s.corrIDs.Store(key, corrID)
	}
}

func (s *BackgroundAcquisitionScheduler) finishEnqueue(ctx context.Context, trackId domain.TrackId, userId shared.UserId, kind ports.JobKind) error {
	if s.closed.Load() {
		s.pending.untrack(trackId.String())
	}
	slog.InfoContext(ctx, "acquisition.scheduling", "track_id", trackId.String(), "user_id", userId.String(), "kind", string(kind))
	return nil
}

func (s *BackgroundAcquisitionScheduler) handleEnqueueError(ctx context.Context, key string, trackId domain.TrackId, kind ports.JobKind, alreadyTracked bool, err error) error {
	if !alreadyTracked {
		s.pending.untrack(key)
	}
	if errors.Is(err, ports.ErrJobKindConflict) {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			"track_id", trackId.String(), "requested_kind", string(kind))
		return ErrTrackJobInFlight
	}
	return err
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
		slog.Error("acquisition.claim_failed", "error", err)
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

	jobCtx, cancelJob := s.jobContextFor(key, job.Attempts)
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
		record := s.log.complete(key, JobCancelled, "shutdown")
		s.recordOutcome(ctx, record)
		slog.WarnContext(ctx, "background acquisition cancelled for shutdown", "track_id", key)
		return
	}
	if err != nil {
		reason := logSafeError(err)
		record := s.log.complete(key, JobFailed, reason)
		s.recordOutcome(ctx, record)
		slog.ErrorContext(ctx, "background acquisition failed", "track_id", key, "error", reason)
		return
	}
	record := s.log.complete(key, JobSucceeded, "")
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

func (s *BackgroundAcquisitionScheduler) heartbeatLoop(ctx context.Context, job ports.Job, cancelJob context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()
	lastSuccess := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.sendHeartbeat(job, &lastSuccess) {
				cancelJob()
				return
			}
		}
	}
}

func (s *BackgroundAcquisitionScheduler) sendHeartbeat(job ports.Job, lastSuccess *time.Time) bool {
	err := s.queue.Heartbeat(context.Background(), job.TrackID, job.Fence, s.leaseDuration)
	if err == nil {
		*lastSuccess = time.Now()
		return true
	}
	if errors.Is(err, ports.ErrLeaseLost) {
		slog.Warn("acquisition.lease_lost", "track_id", job.TrackID.String())
		return false
	}
	if time.Since(*lastSuccess) >= s.leaseDuration {
		slog.Error("acquisition.heartbeat_lease_expired", "track_id", job.TrackID.String(), "error", err)
		return false
	}
	slog.Error("acquisition.heartbeat_failed", "track_id", job.TrackID.String(), "error", err)
	return true
}

func (s *BackgroundAcquisitionScheduler) finishJob(job ports.Job, jobErr error) {
	defer s.pending.untrack(job.TrackID.String())

	if s.baseCtx.Err() != nil {
		err := s.queue.Release(context.Background(), job.TrackID, job.Fence, time.Now())
		s.logQueueOutcome("release", job.TrackID, err)
		return
	}
	if errors.Is(jobErr, errJobPanicked) {
		s.releasePanickedJob(job)
		return
	}
	if errors.Is(jobErr, ErrAcquisitionRetryable) {
		availableAt := time.Now().Add(retryBackoff(job.Attempts))
		err := s.queue.Release(context.Background(), job.TrackID, job.Fence, availableAt)
		s.logQueueOutcome("release", job.TrackID, err)
		return
	}
	err := s.queue.Settle(context.Background(), job.TrackID, job.Fence)
	s.logQueueOutcome("settle", job.TrackID, err)
}

func (s *BackgroundAcquisitionScheduler) releasePanickedJob(job ports.Job) {
	if job.Attempts >= maxAcquisitionAttempts {
		s.refuseQueuedRecovering(job)
	}
	err := s.queue.Release(context.Background(), job.TrackID, job.Fence, time.Now().Add(retryBackoff(job.Attempts)))
	s.logQueueOutcome("release", job.TrackID, err)
}

func (s *BackgroundAcquisitionScheduler) refuseQueuedRecovering(job ports.Job) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("acquisition_panic_fail_track", "track_id", job.TrackID.String(), "panic", r)
		}
	}()
	s.svc.RefuseQueued(context.Background(), job.UserID, job.TrackID)
}

func (s *BackgroundAcquisitionScheduler) logQueueOutcome(op string, trackID domain.TrackId, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ports.ErrLeaseLost) {
		slog.Warn("acquisition."+op+"_lease_lost", "track_id", trackID.String())
		return
	}
	slog.Error("acquisition."+op+"_failed", "track_id", trackID.String(), "error", err)
}

func (s *BackgroundAcquisitionScheduler) logJobPanic(jobCtx context.Context, key string, r any) {
	record := s.log.complete(key, JobFailed, "panic")
	s.recordOutcome(jobCtx, record)
	slog.ErrorContext(jobCtx, "acquisition_panic",
		"track_id", key,
		"panic", r,
		"stack", string(debug.Stack()),
	)
}

func (s *BackgroundAcquisitionScheduler) Status() ports.AcquisitionStatus {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	workers := cap(s.sem)
	if workers < 1 {
		workers = 1
	}
	pending, oldestAge := s.pendingDepth()
	return ports.AcquisitionStatus{
		InFlight:         int(s.inflightCount.Load()),
		Succeeded:        succeeded,
		Failed:           failed,
		Rejected:         s.rejected.Load(),
		VerifySkipped:    s.verifySkipped(),
		Paused:           s.paused.Load(),
		QueueDepth:       pending,
		OldestPendingAge: oldestAge,
		QueueCapacity:    workers,
		Verification:     s.verificationStatus(),
		ActiveJobs:       jobs,
		Recent:           recent,
	}
}

func (s *BackgroundAcquisitionScheduler) pendingDepth() (int, time.Duration) {
	reader, ok := s.queue.(ports.QueueDepthReader)
	if !ok {
		return 0, 0
	}
	pending, oldestAge, err := reader.PendingDepth(s.baseCtx)
	if err != nil {
		slog.Error("acquisition.pending_depth_failed", "error", err)
		return 0, 0
	}
	return pending, oldestAge
}

func (s *BackgroundAcquisitionScheduler) closeAdmission() {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	s.closed.Store(true)
	s.closeOnce.Do(func() { close(s.stop) })
}

func (s *BackgroundAcquisitionScheduler) Shutdown(ctx context.Context) {
	s.closeAdmission()
	defer s.pending.sweep()

	done := make(chan struct{})
	go func() {
		s.jobsWG.Wait()
		close(done)
	}()

	drained := s.awaitJobsWG(done, s.drainBudget, ctx)
	if drained {
		slog.Info("acquisition scheduler drained")
	} else {
		slog.Warn("acquisition drain budget exceeded, cancelling in-flight jobs for release", "budget", s.drainBudget.String())
	}

	s.cancel()
	if !drained {
		drained = s.awaitJobsWG(done, 0, ctx)
		if !drained {
			slog.Warn("acquisition scheduler drain timed out waiting for cancelled jobs to release")
		}
	}

	s.waitRunners(ctx)
	if !drained {
		slog.Warn("acquisition scheduler shutdown ended with jobs still in flight, skipping the outcome wait")
		return
	}
	s.waitOutcomes(ctx)
}

func (s *BackgroundAcquisitionScheduler) awaitJobsWG(done <-chan struct{}, budget time.Duration, ctx context.Context) bool {
	var timerC <-chan time.Time
	if budget > 0 {
		timer := time.NewTimer(budget)
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case <-done:
		return true
	case <-timerC:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) waitRunners(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.runnersWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("acquisition scheduler shutdown context ended before every worker goroutine exited")
	}
}

func (s *BackgroundAcquisitionScheduler) waitOutcomes(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.outcomeWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("acquisition scheduler shutdown context ended before every outcome was recorded")
	}
}
