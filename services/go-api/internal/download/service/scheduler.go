package service

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	runners background.Tracker

	workers int

	cancel context.CancelFunc

	gate *admission
	wake chan struct{}

	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	pollInterval      time.Duration
	drainBudget       time.Duration

	rejected atomic.Uint64

	verification        ports.Verification
	skipCount           func() uint64
	fingerprintVerified func() bool
	log                 *jobLog
	now                 func() time.Time
	finisher            *jobFinisher
	runner              *jobRunner
	claimer             *claimer

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
		gate:                newAdmission(),
		wake:                make(chan struct{}, 1),
		log:                 newJobLog(now),
		now:                 now,
		upgrades:            wiring.Upgrades,
		events:              wiring.Events,
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
	s.runner = &jobRunner{
		svc:               s.svc,
		events:            s.events,
		queue:             s.queue,
		log:               s.log,
		finisher:          s.finisher,
		upgrades:          s.upgrades,
		ticker:            wiring.Ticker,
		now:               s.now,
		heartbeatInterval: s.heartbeatInterval,
		leaseDuration:     s.leaseDuration,
	}
	s.runners.Track(func() { s.notifier.Listen(ctx, s.wake) })
	s.claimer = &claimer{
		gate:          s.gate,
		queue:         s.queue,
		runner:        s.runner,
		wake:          s.wake,
		lanes:         newLaneWakes(),
		timer:         wiring.Timer,
		pollInterval:  s.pollInterval,
		leaseDuration: s.leaseDuration,
	}
	s.runners.Track(s.claimer.fanOutWakes)

	for range s.workers {
		s.runners.Track(func() { s.claimer.runWorker(ctx, s.claimer.claimJob, s.claimer.lanes.acquire) })
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
	claim := func(ctx context.Context) (<-chan struct{}, bool) { return s.claimer.claimFrom(ctx, queue.ClaimUpgrade) }
	for range s.upgrades.workers {
		s.runners.Track(func() { s.claimer.runWorker(ctx, claim, s.claimer.lanes.upgrade) })
	}
}

func (s *BackgroundAcquisitionScheduler) verificationStatus() ports.Verification {
	v := s.verification
	v.FingerprintVerified = s.fingerprintVerified()
	return v
}

func (s *BackgroundAcquisitionScheduler) verifySkipped() uint64 {
	return s.skipCount()
}

func (s *BackgroundAcquisitionScheduler) Pause() { s.gate.paused.Store(true) }

func (s *BackgroundAcquisitionScheduler) Resume() {
	s.gate.paused.Store(false)
	notifyWake(s.wake)
}
