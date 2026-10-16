package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const priorRejectionWindow = 30 * 24 * time.Hour

type RejectionStage string

const (
	RejectionIdentity     RejectionStage = "identity"
	RejectionQualifier    RejectionStage = "qualifier"
	RejectionDownload     RejectionStage = "download"
	RejectionDRM          RejectionStage = ports.UnplayableDRM
	RejectionPreview      RejectionStage = ports.UnplayablePreview
	RejectionDuration     RejectionStage = "duration"
	RejectionUndecodable  RejectionStage = "undecodable"
	RejectionFingerprint  RejectionStage = "fingerprint"
	RejectionNotAttempted RejectionStage = "not_attempted"
	RejectionCurrentFile  RejectionStage = "current_file"
)

type CandidateRejection struct {
	URL    string
	Title  string
	Source acqdomain.SourceName
	Stage  RejectionStage
	Reason string
}

func (ac *AcquisitionContext) recordRejection(url, title string, source acqdomain.SourceName, stage RejectionStage, reason string) {
	ac.Rejections = append(ac.Rejections, CandidateRejection{
		URL:    url,
		Title:  title,
		Source: source,
		Stage:  stage,
		Reason: reason,
	})
}

func summarizeRejections(rejections []CandidateRejection) string {
	if len(rejections) == 0 {
		return ""
	}

	byStage := make(map[string]int, len(rejections))
	for _, r := range rejections {
		byStage[string(r.Stage)]++
	}

	stages := make([]string, 0, len(byStage))
	for stage := range byStage {
		stages = append(stages, stage)
	}
	sort.Strings(stages)

	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, fmt.Sprintf("%d %s", byStage[stage], stage))
	}

	noun := "candidates"
	if len(rejections) == 1 {
		noun = "candidate"
	}
	return fmt.Sprintf("all %d %s rejected (%s)", len(rejections), noun, strings.Join(parts, ", "))
}

func (s RejectionStage) lasting() bool {
	return s != RejectionDownload && s != RejectionNotAttempted && s != RejectionCurrentFile
}

func lastingRejectionRecords(ac *AcquisitionContext) []ports.CandidateRejectionRecord {
	recs := make([]ports.CandidateRejectionRecord, 0, len(ac.Rejections))
	for _, r := range ac.Rejections {
		key := sourceKey(r.URL)
		if !r.Stage.lasting() || key == "" {
			continue
		}
		recs = append(recs, ports.CandidateRejectionRecord{
			TrackID:   ac.Track.ID,
			SourceKey: key,
			Reason:    string(r.Stage),
			Detail:    r.Reason,
			Basis:     ac.rejectionBasis(),
		})
	}
	return recs
}

func (ac *AcquisitionContext) rejectionBasis() ports.RejectionBasis {
	return ports.RejectionBasis{ExpectedSeconds: ac.ExpectedLength(), ReferenceMBID: ac.Identity.MBID}
}

func (s *AcquireTrackAudioService) recordRejections(ctx context.Context, ac *AcquisitionContext) {
	if s.rejections == nil {
		return
	}
	recs := lastingRejectionRecords(ac)
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

func unplayableStage(reason string) RejectionStage {
	if reason == ports.UnplayablePreview {
		return RejectionPreview
	}
	return RejectionDRM
}
