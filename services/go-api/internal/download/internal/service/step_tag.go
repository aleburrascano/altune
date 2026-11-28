package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
)

type TagStep struct {
	tagger ports.AudioTagger
}

func NewTagStep(tagger ports.AudioTagger) (*TagStep, error) {
	if err := missingPort("NewTagStep", "tagger", tagger); err != nil {
		return nil, err
	}
	return &TagStep{tagger: tagger}, nil
}

func (s *TagStep) Name() StepName { return stepNameTag }

func (s *TagStep) Execute(ctx context.Context, ac *AcquisitionContext, downloaded afterDownload) (afterTag, error) {
	isrc, _ := ac.provenISRC()
	tags := domain.TrackTags{
		Title:       ac.Track.Title,
		Artist:      ac.Track.Artist,
		Album:       ac.Track.Album,
		AlbumArtist: ac.Track.AlbumArtist,
		Genre:       ac.Track.Genre,
		Year:        ac.Track.Year,
		TrackNumber: ac.Track.TrackNumber,
		ISRC:        isrc,
	}
	if err := s.tagger.Tag(ctx, ac.TempPath, tags); err != nil {
		slog.WarnContext(ctx, "tagging_failed", logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
	}
	return afterTag(downloaded), nil
}

func (s *TagStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
