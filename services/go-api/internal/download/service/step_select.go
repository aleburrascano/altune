package service

import (
	"altune/go-api/internal/download/internal/core/candidateranking"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() StepName { return stepNameSelect }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSearch) (AfterSelect, error) {
	ranked, rejected := rankAndCollect(ctx, ac.Track, ac.ExpectedLength(), ac.Candidates)
	ac.Rejections = append(ac.Rejections, rejected...)
	if len(ranked) == 0 && ac.SearchUnavailable != nil {
		return AfterSelect{}, fmt.Errorf("no candidates passed matching gates: %w", ac.SearchUnavailable)
	}
	if len(ranked) == 0 {
		return AfterSelect{}, fmt.Errorf("no candidates passed matching gates")
	}
	ac.Ranked = ranked
	best := ranked[0]
	ac.Selected = &best
	return AfterSelect{}, nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}

func logEvaluated(ctx context.Context, track TrackRef, ev candidateranking.Evaluation) {
	c := ev.Candidate
	slog.InfoContext(ctx, "candidate_evaluated",
		logging.KeyTrackID, track.ID,
		"source", c.Source,
		"candidate_title", c.Title,
		"candidate_channel", c.Channel,
		"candidate_duration", c.Duration,
		"candidate_views", c.ViewCount,
		"identity_score", ev.IdentityScore,
		"metadata_rank", ev.MetadataRank,
		"qualifier_distance", ev.QualifierDistance,
		"is_topic", ev.IsTopic,
		"artist_match", ev.ArtistMatch,
		"feature_match", ev.FeatureMatch,
		"explicit", c.Explicit,
		"track_artist", track.Artist,
	)
}

func rankAndCollect(ctx context.Context, track TrackRef, expectedLength float64, candidates []ports.AudioCandidate) ([]ports.AudioCandidate, []rejectedcandidates.Entry) {
	ranked, refused, evaluated := candidateranking.Rank(candidateranking.Track{
		ID:              track.ID,
		Title:           track.Title,
		Artist:          track.Artist,
		ExpectedLength:  expectedLength,
		CatalogExplicit: track.CatalogExplicit,
	}, candidates)
	for _, ev := range evaluated {
		logEvaluated(ctx, track, ev)
	}
	var rejected []rejectedcandidates.Entry
	for _, r := range refused {
		c := r.Candidate
		rejected = append(rejected, rejectedcandidates.Entry{URL: c.URL, Title: c.Title, Source: c.Source, Stage: rejectedcandidates.Stage(r.Stage), Reason: r.Reason})
	}
	return ranked, rejected
}
