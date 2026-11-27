package service

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	Execute(ctx context.Context, job ports.AcquisitionJob) error
	ExecuteReplace(ctx context.Context, job ports.AcquisitionJob) error
	RefuseQueued(ctx context.Context, job ports.AcquisitionJob)
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

	cancel context.CancelFunc

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
	skipCount           func() uint64
	fingerprintVerified func() bool
	log                 *jobLog
	now                 func() time.Time
	finisher            *jobFinisher

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

func NewBackgroundAcquisitionScheduler(
	parent context.Context,
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
	ctx, cancel := background.DetachCancelable(parent)
	s := &BackgroundAcquisitionScheduler{
		svc:                 svc,
		pending:             newPendingTracker(),
		workers:             max(workers, 1),
		cancel:              cancel,
		stop:                make(chan struct{}),
		wake:                make(chan struct{}, 1),
		lanes:               newLaneWakes(),
		log:                 newJobLog(now),
		now:                 now,
		upgrades:            wiring.Upgrades,
		events:              wiring.Events,
		timer:               wiring.Timer,
		ticker:              wiring.Ticker,
		verification:        wiring.Verification,
		notifier:            wiring.Notifier,
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
	s.finisher = newJobFinisher(s.queue, s.pending, s.log, s.svc, wiring.Outcomes, wiring.Unit)
	s.runners.Track(func() { s.notifier.Listen(ctx, s.wake) })
	s.runners.Track(s.fanOutWakes)

	for range s.workers {
		s.runners.Track(func() { s.runWorker(ctx, s.claimJob, s.lanes.acquire) })
	}
	s.startUpgradeWorkers(ctx)
	return s, nil
}

func MustNewBackgroundAcquisitionScheduler(
	parent context.Context,
	svc acquirer,
	workers int,
	now clock.Func,
	wiring SchedulerPorts,
	opts ...func(*BackgroundAcquisitionScheduler),
) *BackgroundAcquisitionScheduler {
	s, err := NewBackgroundAcquisitionScheduler(parent, svc, workers, now, wiring, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *BackgroundAcquisitionScheduler) startUpgradeWorkers(ctx context.Context) {
	if s.upgrades.none {
		return
	}
	queue, ok := s.queue.(ports.UpgradeJobQueue)
	if !ok {
		slog.ErrorContext(ctx, "acquisition.upgrade_lane_unavailable", "reason", "the job queue cannot claim upgrade jobs")
		return
	}
	claim := func(ctx context.Context) (<-chan struct{}, bool) { return s.claimFrom(ctx, queue.ClaimUpgrade) }
	for range s.upgrades.workers {
		s.runners.Track(func() { s.runWorker(ctx, claim, s.lanes.upgrade) })
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

func (s *BackgroundAcquisitionScheduler) runWorker(ctx context.Context, claim func(context.Context) (<-chan struct{}, bool), wake chan struct{}) {
	for {
		if s.closed.Load() {
			return
		}
		done, ok := s.nextJob(ctx, claim)
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

func (s *BackgroundAcquisitionScheduler) nextJob(ctx context.Context, claim func(context.Context) (<-chan struct{}, bool)) (<-chan struct{}, bool) {
	if s.paused.Load() {
		return nil, false
	}
	return claim(ctx)
}

func (s *BackgroundAcquisitionScheduler) claimJob(ctx context.Context) (<-chan struct{}, bool) {
	return s.claimFrom(ctx, s.queue.Claim)
}

type claimFunc func(ctx context.Context, lease time.Duration) (ports.Job, error)

func (s *BackgroundAcquisitionScheduler) claimFrom(ctx context.Context, claim claimFunc) (<-chan struct{}, bool) {
	s.admitMu.RLock()
	defer s.admitMu.RUnlock()
	if s.closed.Load() {
		return nil, false
	}
	job, ok := s.claimNext(ctx, claim)
	if !ok {
		return nil, false
	}
	done := make(chan struct{})
	s.jobs.Track(func() {
		defer close(done)
		s.runClaimedJob(ctx, job)
	})
	return done, true
}

func (s *BackgroundAcquisitionScheduler) claimNext(ctx context.Context, claim claimFunc) (ports.Job, bool) {
	job, err := claim(ctx, s.leaseDuration)
	if errors.Is(err, acqdomain.ErrNoJobAvailable) {
		return ports.Job{}, false
	}
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.claim_failed", logging.KeyError, err)
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
	end := jobEnd{err: jobErr, shutdown: ctx.Err() != nil}
	s.finisher.completeClaimedJob(jobCtx, job.TrackID, end)
	s.stopClaimedJob(cancelJob, heartbeatDone)

	finishCtx, cancelFinish := background.DetachCancelable(jobCtx)
	defer cancelFinish()
	s.finisher.finishJob(finishCtx, job, end)
}

func (s *BackgroundAcquisitionScheduler) startClaimedJob(job ports.Job) ports.AcquisitionJob {
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
	return ports.AcquisitionJob{
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

func (s *BackgroundAcquisitionScheduler) execClaimedJob(ctx context.Context, run acquisitionRun, job ports.AcquisitionJob) error {
	trackId := job.TrackID
	var err error
	if panicked := background.SafeRun(ctx, "acquisition_panic", func() { err = run(ctx, job) }, logging.KeyTrackID, trackId.String()); panicked != nil {
		err = errJobPanicked
	}
	return err
}

var errJobPanicked = errors.New("acquisition job panicked")

type acquisitionRun func(ctx context.Context, job ports.AcquisitionJob) error
