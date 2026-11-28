package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const settleBudget = 10 * time.Second

type outcomeReporter struct {
	ledger ports.LibraryTracks
	events events.Publisher
}

type retryPolicy func(job ports.AcquisitionJob, err error) bool

func neverRetry(ports.AcquisitionJob, error) bool { return false }

func WillRetry(job ports.AcquisitionJob, err error) bool {
	return job.Attempt > 0 && job.Attempt < MaxAcquisitionAttempts && isTransientFailure(err)
}

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return background.Detach(ctx, settleBudget)
}

func (o outcomeReporter) settleExecute(ctx context.Context, err error, ac *AcquisitionContext) error {
	job := ac.Job
	return o.settle(ctx, job, err, WillRetry, func(err error) error {
		return o.reportAcquireFailure(ctx, job, err, ac)
	})
}

func (o outcomeReporter) settleReplace(ctx context.Context, err error, retry retryPolicy, ac *AcquisitionContext) error {
	job := ac.Job
	return o.settle(ctx, job, err, retry, func(err error) error {
		return o.reportReplaceFailure(ctx, job.UserID, job.TrackID, err, ac)
	})
}

func (o outcomeReporter) settle(ctx context.Context, job ports.AcquisitionJob, err error, retry retryPolicy, reportTerminal func(error) error) error {
	timedOut := ownBudgetSpent(ctx)
	if errors.Is(err, domain.ErrLeaseLost) || (!timedOut && job.CancelledByScheduler(ctx)) {
		return o.reportSchedulerCancelledWithoutMarkingFailed(ctx, job.UserID, job.TrackID, err)
	}
	err = ownTimeout(ctx, err)
	if retry(job, err) {
		return o.reportRetryableFailure(ctx, job.UserID, job.TrackID, err)
	}
	return reportTerminal(err)
}

func (o outcomeReporter) reportReplaceFailure(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		logging.KeyTrackID, trackId.String(),
		"replace", true,
		logging.KeyError, redact.LogError(err),
	)
	reason := rejectionAwareReason(ctx, trackId, err, ac)
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	o.events.Publish(settleCtx, userId, events.TrackReplaceFailed{
		TrackID: trackId.String(),
		Reason:  reason,
	})
	return err
}

func (o outcomeReporter) reportSchedulerCancelledWithoutMarkingFailed(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error) error {
	slog.WarnContext(ctx, "track_acquisition_cancelled",
		logging.KeyTrackID, trackId.String(),
		logging.KeyError, redact.LogError(err),
	)
	return err
}

func (o outcomeReporter) reportRetryableFailure(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error) error {
	slog.WarnContext(ctx, "track_acquisition_retryable",
		logging.KeyTrackID, trackId.String(),
		logging.KeyError, redact.LogError(err),
	)
	return fmt.Errorf("%w: %w", ErrAcquisitionRetryable, err)
}

func (o outcomeReporter) reportAcquireFailure(ctx context.Context, job ports.AcquisitionJob, err error, ac *AcquisitionContext) error {
	userId, trackId := job.UserID, job.TrackID
	slog.WarnContext(ctx, "track_acquisition_failed",
		logging.KeyTrackID, trackId.String(),
		"replace", false,
		logging.KeyError, redact.LogError(err),
	)
	reason := rejectionAwareReason(ctx, trackId, err, ac)
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if persistErr := o.failTrack(settleCtx, job.Claim, trackId, userId, reason); errors.Is(persistErr, domain.ErrLeaseLost) {
		return errors.Join(err, persistErr)
	}
	o.events.Publish(settleCtx, userId, events.TrackAcquisitionFailed{
		TrackID: trackId.String(),
		Reason:  reason,
	})
	return err
}

func rejectionAwareReason(ctx context.Context, trackId ids.TrackId, err error, ac *AcquisitionContext) string {
	summary := SummarizeRejections(ac.Rejections)
	if summary == "" {
		return failureReason(err)
	}
	slog.InfoContext(ctx, "acquisition.rejection_summary",
		logging.KeyTrackID, trackId.String(), "summary", summary)
	return downloadfailure.JoinFailureReason(failureCode(err), summary)
}

func (o outcomeReporter) completed(ctx context.Context, userId ids.UserId, trackId ids.TrackId, audioRef string) {
	slog.InfoContext(ctx, "track_acquisition_completed",
		logging.KeyTrackID, trackId.String(),
		"audio_ref", audioRef,
	)
	o.events.Publish(ctx, userId, events.TrackAcquisitionCompleted{
		TrackID:  trackId.String(),
		AudioRef: audioRef,
	})
}

func (o outcomeReporter) markFailed(ctx context.Context, claim ports.ClaimNumber, trackId ids.TrackId, userId ids.UserId, reason string) bool {
	return o.failTrack(ctx, claim, trackId, userId, reason) == nil
}

func (o outcomeReporter) failTrack(ctx context.Context, claim ports.ClaimNumber, trackId ids.TrackId, userId ids.UserId, reason string) error {
	err := o.ledger.Fail(ctx, trackId, userId, claim, reason)
	if errors.Is(err, ports.ErrIllegalTransition) {
		slog.InfoContext(ctx, "mark_failed: track already settled, failure ignored",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return err
	}
	if errors.Is(err, domain.ErrLeaseLost) {
		slog.InfoContext(ctx, "mark_failed: lease lost, failure not persisted",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return err
	}
	if err != nil {
		slog.ErrorContext(ctx, "mark_failed: could not persist failure",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return err
	}
	return nil
}

func (o outcomeReporter) refuseQueued(ctx context.Context, job ports.AcquisitionJob) {
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if !o.markFailed(settleCtx, job.Claim, job.TrackID, job.UserID, string(downloadfailure.FailureAcquisitionRefused)) {
		return
	}
	o.events.Publish(settleCtx, job.UserID, events.TrackAcquisitionFailed{
		TrackID: job.TrackID.String(),
		Reason:  string(downloadfailure.FailureAcquisitionRefused),
	})
}
