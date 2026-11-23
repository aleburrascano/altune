package service

import (
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/rejectionlog"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const priorRejectionWindow = 30 * 24 * time.Hour

func (ac *AcquisitionContext) recordRejection(url, title string, source acqdomain.SourceName, stage rejectionlog.Stage, reason string) {
	ac.appendRejection(rejectionlog.Entry{URL: url, Title: title, Source: source, Stage: stage, Reason: reason})
}

func (ac *AcquisitionContext) recordUnmeasuredRejection(url, title string, source acqdomain.SourceName, stage rejectionlog.Stage, reason string) {
	ac.appendRejection(rejectionlog.Entry{URL: url, Title: title, Source: source, Stage: stage, Reason: reason, Unmeasured: true})
}

func (ac *AcquisitionContext) appendRejection(r rejectionlog.Entry) {
	ac.Rejections = append(ac.Rejections, r)
}

func (ac *AcquisitionContext) rejectionBasis() ports.RejectionBasis {
	return ports.RejectionBasis{ExpectedSeconds: ac.ExpectedLength(), ReferenceMBID: ac.Identity.MBID}
}

func (s *AcquireTrackAudioService) recordRejections(ctx context.Context, ac *AcquisitionContext) {
	if s.rejections == nil {
		return
	}
	recs := rejectionlog.Lasting(ac.Rejections, ac.Track.ID, ac.rejectionBasis())
	if len(recs) == 0 {
		return
	}
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if err := s.rejections.Record(settleCtx, recs); err != nil {
		slog.WarnContext(ctx, "acquisition.rejection_record_failed",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
	}
}

func (s *AcquireTrackAudioService) loadPriorRejections(ctx context.Context, ac *AcquisitionContext) {
	if s.rejections == nil {
		return
	}
	prior, err := s.rejections.Active(ctx, ac.Track.ID, time.Now().Add(-priorRejectionWindow))
	if err != nil {
		slog.WarnContext(ctx, "acquisition.rejection_load_failed",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
		return
	}
	ac.PriorRejections = prior
}
