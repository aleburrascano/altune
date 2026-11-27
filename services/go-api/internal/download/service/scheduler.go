package service

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service/jobrun"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
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
	Execute(ctx context.Context, job jobrun.Job) error
	ExecuteReplace(ctx context.Context, job jobrun.Job) error
	RefuseQueued(ctx context.Context, job jobrun.Job)
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
	timer             clock.TimerFunc
	ticker            clock.TickerFunc
	drainBudget       time.Duration

	inflightCount atomic.Int64
	rejected      atomic.Uint64

	verification        ports.Verification
	unit                random.Unit
	skipCount           func() uint64
	fingerprintVerified func() bool
	log                 *jobLog
	now                 func() time.Time
	outcomes            ports.OutcomeRecorder
	outcomeRecords      background.Tracker

	upgrades UpgradeLane
}

type QueueFactory func(wake chan<- struct{}, now clock.Func) ports.DownloadQueue

func InMemoryQueue() QueueFactory {
	return func(wake chan<- struct{}, now clock.Func) ports.DownloadQueue { return inmemoryqueue.New(wake, now) }
}

func QueueOf(queue ports.DownloadQueue) QueueFactory {
	return func(chan<- struct{}, clock.Func) ports.DownloadQueue { return queue }
}

type UpgradeLane struct {
	runner   upgradeRunner
	workers  int
	declared bool
	none     bool
}

var ErrUpgradeWorkersBelowOne = errors.New("an upgrade lane needs at least one worker")

func UpgradeLaneOf(runner upgradeRunner, workers int) (UpgradeLane, error) {
	if err := missingPort("UpgradeLaneOf", "runner", runner); err != nil {
		return UpgradeLane{}, err
	}
	if workers < 1 {
		return UpgradeLane{}, fmt.Errorf("UpgradeLaneOf: %w: got %d", ErrUpgradeWorkersBelowOne, workers)
	}
	return UpgradeLane{runner: runner, workers: workers, declared: true}, nil
}

func MustUpgradeLaneOf(runner upgradeRunner, workers int) UpgradeLane {
	lane, err := UpgradeLaneOf(runner, workers)
	if err != nil {
		panic(err)
	}
	return lane
}

func (l UpgradeLane) require(owner string) error {
	if l.declared {
		return nil
	}
	return errMissingPort(owner, "Upgrades")
}

func NoUpgradeLane() UpgradeLane { return UpgradeLane{declared: true, none: true} }

type SchedulerPorts struct {
	Queue               QueueFactory
	Upgrades            UpgradeLane
	Notifier            ports.JobNotifier
	Outcomes            ports.OutcomeRecorder
	Events              events.Publisher
	VerifySkips         func() uint64
	FingerprintVerified func() bool
	Timer               clock.TimerFunc
	Ticker              clock.TickerFunc
	Unit                random.Unit
	Verification        ports.Verification
}

func (p SchedulerPorts) validate(owner string) error {
	return errors.Join(
		missingPort(owner, "Queue", p.Queue),
		p.Upgrades.require(owner),
		missingPort(owner, "Notifier", p.Notifier),
		missingPort(owner, "Outcomes", p.Outcomes),
		missingPort(owner, "Events", p.Events),
		missingPort(owner, "VerifySkips", p.VerifySkips),
		missingPort(owner, "FingerprintVerified", p.FingerprintVerified),
		missingPort(owner, "Timer", p.Timer),
		missingPort(owner, "Ticker", p.Ticker),
		missingPort(owner, "Unit", p.Unit),
	)
}

type ProcessRoot struct{ ctx context.Context }

func NewProcessRoot(ctx context.Context) ProcessRoot { return ProcessRoot{ctx: ctx} }

func NewBackgroundAcquisitionScheduler(
	processRoot ProcessRoot,
	svc acquirer,
	workers int,
	now clock.Func,
	wiring SchedulerPorts,
	opts ...func(*BackgroundAcquisitionScheduler),
) (*BackgroundAcquisitionScheduler, error) {
	const owner = "NewBackgroundAcquisitionScheduler"
	if err := errors.Join(missingPort(owner, "clock", now), wiring.validate(owner)); err != nil {
		return nil, err
	}
	ctx, cancel := background.DetachCancelable(processRoot.ctx)
	s := &BackgroundAcquisitionScheduler{
		svc:                 svc,
		pending:             newPendingTracker(),
		workers:             max(workers, 1),
		cancel:              cancel,
		baseCtx:             ctx,
		stop:                make(chan struct{}),
		wake:                make(chan struct{}, 1),
		lanes:               newLaneWakes(),
		log:                 newJobLog(now),
		now:                 now,
		upgrades:            wiring.Upgrades,
		events:              wiring.Events,
		timer:               wiring.Timer,
		ticker:              wiring.Ticker,
		unit:                wiring.Unit,
		verification:        wiring.Verification,
		notifier:            wiring.Notifier,
		outcomes:            wiring.Outcomes,
		skipCount:           wiring.VerifySkips,
		fingerprintVerified: wiring.FingerprintVerified,
		leaseDuration:       defaultLeaseDuration,
		heartbeatInterval:   defaultHeartbeatInterval,
		pollInterval:        defaultPollInterval,
		drainBudget:         defaultDrainBudget,
	}
	for _, opt := range opts {
		opt(s)
	}
	logVerification(ctx, s.verification)
	s.queue = wiring.Queue(s.wake, s.now)
	s.runners.Track(func() { s.notifier.Listen(s.baseCtx, s.wake) })
	s.runners.Track(s.fanOutWakes)

	for range s.workers {
		s.runners.Track(func() { s.runWorker(s.claimJob, s.lanes.acquire) })
	}
	s.startUpgradeWorkers()
	return s, nil
}

func MustNewBackgroundAcquisitionScheduler(
	processRoot ProcessRoot,
	svc acquirer,
	workers int,
	now clock.Func,
	wiring SchedulerPorts,
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	s, err := NewBackgroundAcquisitionScheduler(processRoot, svc, workers, now, wiring, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *BackgroundAcquisitionScheduler) startUpgradeWorkers() {
	if s.upgrades.none {
		return
	}
	queue, ok := s.queue.(ports.UpgradeJobQueue)
	if !ok {
		slog.ErrorContext(s.baseCtx, "acquisition.upgrade_lane_unavailable", "reason", "the job queue cannot claim upgrade jobs")
		return
	}
	claim := func() (<-chan struct{}, bool) { return s.claimFrom(queue.ClaimUpgrade) }
	for range s.upgrades.workers {
		s.runners.Track(func() { s.runWorker(claim, s.lanes.upgrade) })
	}
}

func (s *BackgroundAcquisitionScheduler) leaseFence(job ports.Job) func(context.Context) error {
	verifier, ok := s.queue.(ports.UpgradeJobQueue)
	if !ok || job.Kind != ports.JobKindUpgrade {
		return nil
	}
	return func(ctx context.Context) error {
		return verifier.VerifyLease(ctx, job.TrackID, job.ClaimNumber)
	}
}

func (s *BackgroundAcquisitionScheduler) eventsFor(kind ports.JobKind) events.Publisher {
	if kind == ports.JobKindUpgrade {
		return events.NoopPublisher()
	}
	return s.events
}

func (s *BackgroundAcquisitionScheduler) verificationStatus() ports.Verification {
	v := s.verification
	v.FingerprintVerified = s.fingerprintVerified()
	return v
}

func (s *BackgroundAcquisitionScheduler) verifySkipped() uint64 {
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
		if !ok && !s.idleWait(wake) {
			return
		}
		if !ok {
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
		s.runClaimedJob(s.baseCtx, job)
	})
	return done, true
}

func (s *BackgroundAcquisitionScheduler) claimNext(claim claimFunc) (ports.Job, bool) {
	job, err := claim(s.baseCtx, s.leaseDuration)
	if errors.Is(err, acqdomain.ErrNoJobAvailable) {
		return ports.Job{}, false
	}
	if err != nil {
		slog.ErrorContext(s.baseCtx, "acquisition.claim_failed", logging.KeyError, err)
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
	fires, stop := s.timer(s.pollInterval)
	defer stop()
	select {
	case <-wake:
		return true
	case <-fires:
		return true
	case <-s.stop:
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) runClaimedJob(ctx context.Context, job ports.Job) {
	run := s.acquisitionRunFor(job.Kind)

	jobCtx, cancelJob := s.jobContextFor(ctx, job)
	defer cancelJob()

	heartbeatDone := make(chan struct{})
	background.Go(jobCtx, "acquisition.heartbeat_panic", func() { s.heartbeatLoop(jobCtx, job, cancelJob, heartbeatDone) })

	work := s.startClaimedJob(job)
	jobErr := s.execClaimedJob(jobCtx, run, work)
	s.stopClaimedJob(cancelJob, heartbeatDone)

	finishCtx, cancelFinish := s.finishContextOf(jobCtx)
	defer cancelFinish()
	s.finishJob(finishCtx, job, jobErr)
}

func (s *BackgroundAcquisitionScheduler) finishContextOf(jobCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := background.DetachCancelable(s.baseCtx)
	return logging.ContextWithCorrelationID(ctx, logging.CorrelationIDFromContext(jobCtx)), cancel
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

func (s *BackgroundAcquisitionScheduler) startClaimedJob(job ports.Job) jobrun.Job {
	trackId := job.TrackID
	s.inflightCount.Add(1)
	s.log.register(trackId, "")
	s.log.markRunning(trackId)
	s.log.update(trackId, func(r *ports.JobRecord) {
		r.Kind = job.Kind
		r.Run = job.Run
	})
	reporter := schedulerJobReporter{log: s.log, events: s.eventsFor(job.Kind), trackID: trackId, userId: job.UserID}
	reporter.Timing("queue", job.Waited)
	return jobrun.Job{
		UserID:         job.UserID,
		TrackID:        job.TrackID,
		Attempt:        job.Run,
		Claim:          job.ClaimNumber,
		SchedulerOwned: true,
		Reporter:       reporter,
		CommitCheck:    s.leaseFence(job),
	}
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
		return s.upgrades.runner.ExecuteUpgrade
	case ports.JobKindAcquire:
		return s.svc.Execute
	}
	return s.svc.Execute
}

func (s *BackgroundAcquisitionScheduler) jobContextFor(ctx context.Context, job ports.Job) (context.Context, context.CancelFunc) {
	jobCtx, cancelJob := context.WithCancel(ctx)
	if job.CorrelationID != "" {
		jobCtx = logging.ContextWithCorrelationID(jobCtx, job.CorrelationID)
	}
	return jobCtx, cancelJob
}

func (s *BackgroundAcquisitionScheduler) execClaimedJob(ctx context.Context, run acquisitionRun, job jobrun.Job) error {
	trackId := job.TrackID
	var err error
	if panicked := background.SafeRun(ctx, "acquisition_panic", func() { err = run(ctx, job) }, logging.KeyTrackID, trackId.String()); panicked != nil {
		err = errJobPanicked
	}
	s.completeClaimedJob(ctx, trackId, err)
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

func (s *BackgroundAcquisitionScheduler) completeClaimedJob(ctx context.Context, trackId ids.TrackId, err error) {
	defer background.RecoverContext(ctx, "acquisition.job_completion_panic", logging.KeyTrackID, trackId.String())
	done := s.classifyJob(ctx, err)
	record := s.log.complete(trackId, done.state, done.reason)
	if done.state != ports.JobRetrying {
		s.recordOutcome(ctx, record)
	}
	if done.message != "" {
		slog.Log(ctx, done.level, done.message, append([]any{logging.KeyTrackID, trackId.String()}, done.attrs...)...)
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
	case err != nil && ctx.Err() != nil:
		return leaseLost
	case err != nil:
		reason := redact.LogError(err)
		return jobCompletion{state: ports.JobFailed, reason: reason, level: slog.LevelError, message: "background acquisition failed", attrs: []any{logging.KeyError, reason}}
	default:
		return jobCompletion{state: ports.JobSucceeded}
	}
}

func (s *BackgroundAcquisitionScheduler) recordOutcome(ctx context.Context, record ports.JobRecord) {
	if record.TrackID == "" {
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
				logging.KeyTrackID, record.TrackID, logging.KeyError, redact.LogError(err))
		}
	})
}

var errJobPanicked = errors.New("acquisition job panicked")

type acquisitionRun func(ctx context.Context, job jobrun.Job) error

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
		s.logQueueOutcome(ctx, "release", job.TrackID, s.releaseWithin(ctx, job, retryBackoff(job.Run, s.unit)))
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
	s.logQueueOutcome(ctx, "release", job.TrackID, s.releaseWithin(ctx, job, retryBackoff(job.Run, s.unit)))
}

func (s *BackgroundAcquisitionScheduler) refuseQueuedRecovering(ctx context.Context, job ports.Job) {
	defer background.RecoverContext(ctx, "acquisition_panic_fail_track", logging.KeyTrackID, job.TrackID.String())
	s.svc.RefuseQueued(ctx, jobrun.Job{UserID: job.UserID, TrackID: job.TrackID, Claim: job.ClaimNumber})
}

func (s *BackgroundAcquisitionScheduler) logQueueOutcome(ctx context.Context, op string, trackID ids.TrackId, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, acqdomain.ErrLeaseLost) {
		slog.WarnContext(ctx, "acquisition."+op+"_lease_lost", logging.KeyTrackID, trackID.String())
		return
	}
	slog.ErrorContext(ctx, "acquisition."+op+"_failed", logging.KeyTrackID, trackID.String(), logging.KeyError, err)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
