package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"log/slog"
)

type rerecording struct {
	finder           ports.RerecordingFinder
	resolveIdentity  func(context.Context, *AcquisitionContext)
	recordRejections func(context.Context, *AcquisitionContext)
}

func (s *pipeline) rerecording() rerecording {
	return rerecording{finder: s.rerecordings, resolveIdentity: s.resolveIdentity, recordRejections: s.recordRejections}
}

func (r rerecording) take(ctx context.Context, steps Pipeline, ac *AcquisitionContext, originalErr error) error {
	originalMBID := cmp.Or(ac.Identity.MBID, ac.Track.CatalogMBID)
	if !r.mayTake(ctx, ac, originalErr) || originalMBID == "" {
		return originalErr
	}
	label, found := r.find(ctx, ac, originalMBID)
	if !found {
		return originalErr
	}
	err := r.runPass(ctx, steps, ac, label)
	if err == nil {
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "taken", nil)
		return nil
	}
	logRerecordingTier(ctx, ac, originalMBID, label.MBID, "pass_failed", err)
	if ctx.Err() != nil {
		return err
	}
	return originalErr
}

func (r rerecording) runPass(ctx context.Context, steps Pipeline, ac *AcquisitionContext, label domain.TrackLabel) error {
	pass := ac.Snapshot()
	pass.retargetTo(label)
	r.resolveIdentity(ctx, &pass)
	if err := RunPipeline(ctx, steps, &pass); err != nil {
		return err
	}
	r.recordRejections(ctx, ac)
	pass.Rejections = pass.Rejections[len(ac.Rejections):]
	*ac = pass
	return nil
}

func (r rerecording) mayTake(ctx context.Context, ac *AcquisitionContext, err error) bool {
	return noSourceHasIt(err) && ctx.Err() == nil && ac.Replace.allowsRerecording()
}

func (r rerecording) find(ctx context.Context, ac *AcquisitionContext, originalMBID string) (domain.TrackLabel, bool) {
	label, found, err := r.finder.FindRerecording(ctx, domain.RerecordingQuery{OriginalMBID: originalMBID})
	switch {
	case err != nil:
		logRerecordingTier(ctx, ac, originalMBID, "", "finder_failed", err)
	case !found:
		logRerecordingTier(ctx, ac, originalMBID, "", "none", nil)
	case label.MBID == ac.Track.CatalogMBID:
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "already_the_rerecording", nil)
	default:
		return label, true
	}
	return domain.TrackLabel{}, false
}

func logRerecordingTier(ctx context.Context, ac *AcquisitionContext, originalMBID, rerecordingMBID, outcome string, err error) {
	attrs := []any{logging.KeyTrackID, ac.Track.ID, "original_mbid", originalMBID, "rerecording_mbid", rerecordingMBID, "outcome", outcome}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.rerecording_tier", append(attrs, logging.KeyError, redact.LogError(err))...)
		return
	}
	slog.InfoContext(ctx, "acquisition.rerecording_tier", attrs...)
}
