package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
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

type retryPolicy func(jobCtx context.Context, err error) bool

func neverRetry(context.Context, error) bool { return false }

func WillRetry(jobCtx context.Context, err error) bool {
	attempt := jobAttemptFrom(jobCtx)
	return attempt > 0 && attempt < MaxAcquisitionAttempts && isTransientFailure(err)
}

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return background.Detach(ctx, settleBudget)
}

func (o outcomeReporter) settleExecute(ctx, jobCtx context.Context, userId ids.UserId, trackId ids.TrackId, err error, ac *AcquisitionContext) error {
	return o.settle(ctx, jobCtx, userId, trackId, err, WillRetry, func(err error) error {
		return o.reportAcquireFailure(ctx, userId, trackId, err, ac)
	})
}

func (o outcomeReporter) settleReplace(ctx, jobCtx context.Context, userId ids.UserId, trackId ids.TrackId, err error, retry retryPolicy, ac *AcquisitionContext) error {
	return o.settle(ctx, jobCtx, userId, trackId, err, retry, func(err error) error {
		return o.reportReplaceFailure(ctx, userId, trackId, err, ac)
	})
}

func (o outcomeReporter) settle(ctx, jobCtx context.Context, userId ids.UserId, trackId ids.TrackId, err error, retry retryPolicy, reportTerminal func(error) error) error {
	if errors.Is(err, acqdomain.ErrLeaseLost) {
		return o.reportSchedulerCancelledWithoutMarkingFailed(ctx, userId, trackId, err)
	}
	if schedulerCancelledJobContext(jobCtx) {
		return o.reportSchedulerCancelledWithoutMarkingFailed(ctx, userId, trackId, err)
	}
	err = ownTimeout(jobCtx, ctx, err)
	if retry(jobCtx, err) {
		return o.reportRetryableFailure(ctx, userId, trackId, err)
	}
	return reportTerminal(err)
}

func (o outcomeReporter) reportReplaceFailure(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		logging.KeyTrackID, trackId.String(),
		logging.KeyUserID, userId.String(),
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
		logging.KeyUserID, userId.String(),
		logging.KeyError, redact.LogError(err),
	)
	return err
}

func (o outcomeReporter) reportRetryableFailure(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error) error {
	slog.WarnContext(ctx, "track_acquisition_retryable",
		logging.KeyTrackID, trackId.String(),
		logging.KeyUserID, userId.String(),
		logging.KeyError, redact.LogError(err),
	)
	return fmt.Errorf("%w: %w", ErrAcquisitionRetryable, err)
}

func (o outcomeReporter) reportAcquireFailure(ctx context.Context, userId ids.UserId, trackId ids.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		logging.KeyTrackID, trackId.String(),
		logging.KeyUserID, userId.String(),
		"replace", false,
		logging.KeyError, redact.LogError(err),
	)
	reason := rejectionAwareReason(ctx, trackId, err, ac)
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if persistErr := o.failTrack(settleCtx, trackId, userId, reason); errors.Is(persistErr, acqdomain.ErrLeaseLost) {
		return errors.Join(err, persistErr)
	}
	o.events.Publish(settleCtx, userId, events.TrackAcquisitionFailed{
		TrackID: trackId.String(),
		Reason:  reason,
	})
	return err
}

func rejectionAwareReason(ctx context.Context, trackId ids.TrackId, err error, ac *AcquisitionContext) string {
	summary := rejectedcandidates.Summary(ac.Rejections)
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
		logging.KeyUserID, userId.String(),
		"audio_ref", audioRef,
	)
	o.events.Publish(ctx, userId, events.TrackAcquisitionCompleted{
		TrackID:  trackId.String(),
		AudioRef: audioRef,
	})
}

func (o outcomeReporter) markFailed(ctx context.Context, trackId ids.TrackId, userId ids.UserId, reason string) bool {
	return o.failTrack(ctx, trackId, userId, reason) == nil
}

func (o outcomeReporter) failTrack(ctx context.Context, trackId ids.TrackId, userId ids.UserId, reason string) error {
	err := o.ledger.Fail(ctx, trackId, userId, jobFenceFrom(ctx), reason)
	if errors.Is(err, ports.ErrIllegalTransition) {
		slog.InfoContext(ctx, "mark_failed: track already settled, failure ignored",
			logging.KeyTrackID, trackId.String(), logging.KeyError, redact.LogError(err))
		return err
	}
	if errors.Is(err, acqdomain.ErrLeaseLost) {
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

func (o outcomeReporter) refuseQueued(ctx context.Context, userId ids.UserId, trackId ids.TrackId) {
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if !o.markFailed(settleCtx, trackId, userId, string(downloadfailure.FailureAcquisitionRefused)) {
		return
	}
	o.events.Publish(settleCtx, userId, events.TrackAcquisitionFailed{
		TrackID: trackId.String(),
		Reason:  string(downloadfailure.FailureAcquisitionRefused),
	})
}
