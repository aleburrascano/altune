package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

type jobRunner struct {
	svc               acquirer
	events            events.Publisher
	queue             ports.DownloadQueue
	log               *jobLog
	finisher          *jobFinisher
	upgrades          UpgradeLane
	ticker            clock.TickerFunc
	now               func() time.Time
	heartbeatInterval time.Duration
	leaseDuration     time.Duration
	inflightCount     atomic.Int64
}

func (r *jobRunner) inFlight() int { return int(r.inflightCount.Load()) }

func (r *jobRunner) leaseFence(job ports.Job) func(context.Context) error {
	if job.Kind != ports.JobKindUpgrade {
		return nil
	}
	return func(ctx context.Context) error {
		return r.queue.VerifyLease(ctx, job.TrackID, job.ClaimNumber)
	}
}

func (r *jobRunner) eventsFor(kind ports.JobKind) events.Publisher {
	if kind == ports.JobKindUpgrade {
		return events.NoopPublisher()
	}
	return r.events
}

func (r *jobRunner) runClaimedJob(ctx context.Context, job ports.Job) {
	run := r.acquisitionRunFor(job.Kind)

	jobCtx, cancelJob := r.jobContextFor(ctx, job)
	defer cancelJob()

	heartbeatDone := make(chan struct{})
	background.Go(jobCtx, "acquisition.heartbeat_panic", func() { r.heartbeatLoop(jobCtx, job, cancelJob, heartbeatDone) })

	work := r.startClaimedJob(job)
	jobErr := r.execClaimedJob(jobCtx, run, work)
	end := jobEnd{err: jobErr, shutdown: ctx.Err() != nil}
	r.finisher.completeClaimedJob(jobCtx, job.TrackID, end)
	r.stopClaimedJob(cancelJob, heartbeatDone)

	finishCtx, cancelFinish := background.DetachCancelable(jobCtx)
	defer cancelFinish()
	r.finisher.finishJob(finishCtx, job, end)
}

func (r *jobRunner) startClaimedJob(job ports.Job) ports.AcquisitionJob {
	trackId := job.TrackID
	r.inflightCount.Add(1)
	r.log.register(trackId, "")
	r.log.markRunning(trackId)
	r.log.update(trackId, func(rec *ports.JobRecord) {
		rec.Kind = job.Kind
		rec.Run = job.Run
	})
	reporter := schedulerJobReporter{log: r.log, events: r.eventsFor(job.Kind), trackID: trackId, userId: job.UserID}
	reporter.Timing("queue", job.Waited)
	return ports.AcquisitionJob{
		UserID:         job.UserID,
		TrackID:        job.TrackID,
		Attempt:        job.Run,
		Claim:          job.ClaimNumber,
		SchedulerOwned: true,
		Reporter:       reporter,
		CommitCheck:    r.leaseFence(job),
	}
}

func (r *jobRunner) stopClaimedJob(cancelJob context.CancelFunc, heartbeatDone <-chan struct{}) {
	cancelJob()
	<-heartbeatDone
	r.inflightCount.Add(-1)
}

func (r *jobRunner) acquisitionRunFor(kind ports.JobKind) acquisitionRun {
	switch kind {
	case ports.JobKindReplace:
		return r.svc.ExecuteReplace
	case ports.JobKindUpgrade:
		return r.upgrades.runner.ExecuteUpgrade
	case ports.JobKindAcquire:
		return r.svc.Execute
	}
	return r.svc.Execute
}

func (r *jobRunner) jobContextFor(ctx context.Context, job ports.Job) (context.Context, context.CancelFunc) {
	jobCtx, cancelJob := context.WithCancel(ctx)
	if job.CorrelationID != "" {
		jobCtx = logging.ContextWithCorrelationID(jobCtx, job.CorrelationID)
	}
	return jobCtx, cancelJob
}

func (r *jobRunner) execClaimedJob(ctx context.Context, run acquisitionRun, job ports.AcquisitionJob) error {
	trackId := job.TrackID
	var err error
	if panicked := background.SafeRun(ctx, "acquisition_panic", func() { err = run(ctx, job) }, logging.KeyTrackID, trackId.String()); panicked != nil {
		err = errJobPanicked
	}
	return err
}

var errJobPanicked = errors.New("acquisition job panicked")

type acquisitionRun func(ctx context.Context, job ports.AcquisitionJob) error

func (r *jobRunner) heartbeatLoop(ctx context.Context, job ports.Job, cancelJob context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticks, stop := r.ticker(r.heartbeatInterval)
	defer stop()
	lastSuccess := r.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if !r.sendHeartbeat(ctx, job, &lastSuccess) {
				cancelJob()
				return
			}
		}
	}
}

func (r *jobRunner) sendHeartbeat(jobCtx context.Context, job ports.Job, lastSuccess *time.Time) bool {
	ctx, cancel := background.Detach(jobCtx, queueCallBudget)
	defer cancel()
	err := r.queue.Heartbeat(ctx, job.TrackID, job.ClaimNumber, r.leaseDuration)
	if err == nil {
		*lastSuccess = r.now()
		return true
	}
	if errors.Is(err, acqdomain.ErrLeaseLost) {
		slog.WarnContext(ctx, "acquisition.lease_lost", logging.KeyTrackID, job.TrackID.String())
		return false
	}
	if r.now().Sub(*lastSuccess) >= r.leaseDuration {
		slog.ErrorContext(ctx, "acquisition.heartbeat_lease_expired", logging.KeyTrackID, job.TrackID.String(), logging.KeyError, err)
		return false
	}
	slog.ErrorContext(ctx, "acquisition.heartbeat_failed", logging.KeyTrackID, job.TrackID.String(), logging.KeyError, err)
	return true
}
