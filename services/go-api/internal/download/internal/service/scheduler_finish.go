package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"
)

type queueRefuser interface {
	RefuseQueued(ctx context.Context, job ports.AcquisitionJob)
}

type jobFinisher struct {
	queue          ports.DownloadQueue
	pending        *pendingTracker
	log            *jobLog
	refuser        queueRefuser
	outcomes       ports.OutcomeRecorder
	unit           random.Unit
	outcomeRecords background.Tracker
}

func newJobFinisher(queue ports.DownloadQueue, pending *pendingTracker, log *jobLog, refuser queueRefuser, outcomes ports.OutcomeRecorder, unit random.Unit) *jobFinisher {
	return &jobFinisher{queue: queue, pending: pending, log: log, refuser: refuser, outcomes: outcomes, unit: unit}
}

func (f *jobFinisher) releaseWithin(ctx context.Context, job ports.Job, delay time.Duration) error {
	queueCtx, cancel := background.Detach(ctx, queueCallBudget)
	defer cancel()
	return f.queue.Release(queueCtx, job.TrackID, job.ClaimNumber, delay)
}

func (f *jobFinisher) settleWithin(ctx context.Context, job ports.Job) error {
	queueCtx, cancel := background.Detach(ctx, queueCallBudget)
	defer cancel()
	return f.queue.Settle(queueCtx, job.TrackID, job.ClaimNumber)
}

type jobEnd struct {
	err      error
	shutdown bool
}

const LeaseLostReason = "lease_lost"

type jobCompletion struct {
	state   ports.JobState
	reason  string
	level   slog.Level
	message string
	attrs   []any
}

func (f *jobFinisher) completeClaimedJob(ctx context.Context, trackId ids.TrackId, end jobEnd) {
	defer background.RecoverContext(ctx, "acquisition.job_completion_panic", logging.KeyTrackID, trackId.String())
	done := f.classifyJob(ctx, end)
	record := f.log.complete(trackId, done.state, done.reason)
	if done.state != ports.JobRetrying {
		f.recordOutcome(ctx, record)
	}
	if done.message != "" {
		slog.Log(ctx, done.level, done.message, append([]any{logging.KeyTrackID, trackId.String()}, done.attrs...)...)
	}
}

var leaseLost = jobCompletion{state: ports.JobCancelled, reason: LeaseLostReason, level: slog.LevelWarn, message: "background acquisition cancelled after lease loss"}

func (f *jobFinisher) classifyJob(ctx context.Context, end jobEnd) jobCompletion {
	err := end.err
	switch {
	case errors.Is(err, errJobPanicked):
		return jobCompletion{state: ports.JobFailed, reason: "panic"}
	case err != nil && end.shutdown:
		return jobCompletion{state: ports.JobCancelled, reason: "shutdown", level: slog.LevelWarn, message: "background acquisition cancelled for shutdown"}
	case errors.Is(err, ErrKept):
		return jobCompletion{state: ports.JobKept}
	case errors.Is(err, domain.ErrLeaseLost):
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

func (f *jobFinisher) recordOutcome(ctx context.Context, record ports.JobRecord) {
	if record.TrackID == "" {
		return
	}
	var sourceHost string
	if record.State == ports.JobSucceeded {
		sourceHost = hostOf(record.ResolvedSource)
	}
	f.outcomeRecords.Track(func() {
		recCtx, cancel := background.Detach(ctx, OutcomeRecordTimeout)
		defer cancel()
		err := f.outcomes.Record(recCtx, ports.AcquisitionOutcome{
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

func (f *jobFinisher) finishJob(ctx context.Context, job ports.Job, end jobEnd) {
	defer f.pending.untrack(job.TrackID.String())

	if end.shutdown {
		f.logQueueOutcome(ctx, "release", job.TrackID, f.releaseWithin(ctx, job, 0))
		return
	}
	if errors.Is(end.err, errJobPanicked) {
		f.releasePanickedJob(ctx, job)
		return
	}
	if errors.Is(end.err, ErrAcquisitionRetryable) {
		f.logQueueOutcome(ctx, "release", job.TrackID, f.releaseWithin(ctx, job, retryBackoff(job.Run, f.unit)))
		return
	}
	f.logQueueOutcome(ctx, "settle", job.TrackID, f.settleWithin(ctx, job))
}

func (f *jobFinisher) releasePanickedJob(ctx context.Context, job ports.Job) {
	if job.Run >= MaxAcquisitionAttempts {
		f.refuseQueuedRecovering(ctx, job)
		f.logQueueOutcome(ctx, "settle", job.TrackID, f.settleWithin(ctx, job))
		return
	}
	f.logQueueOutcome(ctx, "release", job.TrackID, f.releaseWithin(ctx, job, retryBackoff(job.Run, f.unit)))
}

func (f *jobFinisher) refuseQueuedRecovering(ctx context.Context, job ports.Job) {
	defer background.RecoverContext(ctx, "acquisition_panic_fail_track", logging.KeyTrackID, job.TrackID.String())
	f.refuser.RefuseQueued(ctx, ports.AcquisitionJob{UserID: job.UserID, TrackID: job.TrackID, Claim: job.ClaimNumber})
}

func (f *jobFinisher) logQueueOutcome(ctx context.Context, op string, trackID ids.TrackId, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, domain.ErrLeaseLost) {
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
