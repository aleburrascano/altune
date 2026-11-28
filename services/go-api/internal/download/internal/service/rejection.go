package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const priorRejectionWindow = 30 * 24 * time.Hour

func (ac *AcquisitionContext) recordRejection(url, title string, source domain.SourceName, stage RejectionStage, reason string) {
	ac.appendRejection(RejectionEntry{URL: url, Title: title, Source: source, Stage: stage, Reason: reason})
}

func (ac *AcquisitionContext) recordUnmeasuredRejection(url, title string, source domain.SourceName, stage RejectionStage, reason string) {
	ac.appendRejection(RejectionEntry{URL: url, Title: title, Source: source, Stage: stage, Reason: reason, Unmeasured: true})
}

func (ac *AcquisitionContext) appendRejection(r RejectionEntry) {
	ac.Rejections = append(ac.Rejections, r)
}

func (ac *AcquisitionContext) rejectionBasis() ports.RejectionBasis {
	return ports.RejectionBasis{ExpectedSeconds: ac.ExpectedLength(), ReferenceMBID: ac.Identity.MBID}
}

func (s *pipeline) recordRejections(ctx context.Context, ac *AcquisitionContext) {
	recs := LastingRejections(ac.Rejections, ac.Track.ID, ac.rejectionBasis())
	if len(recs) == 0 {
		return
	}
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if err := s.rejections.Record(settleCtx, recs); err != nil {
		slog.WarnContext(ctx, "acquisition.rejection_record_failed",
			logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
	}
}

func (s *pipeline) loadPriorRejections(ctx context.Context, trackId ids.TrackId, ac *AcquisitionContext) {
	prior, err := s.rejections.Active(ctx, trackId, s.now().Add(-priorRejectionWindow))
	if err != nil {
		slog.WarnContext(ctx, "acquisition.rejection_load_failed",
			logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
		return
	}
	ac.PriorRejections = prior
}
