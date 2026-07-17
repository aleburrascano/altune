package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/acquisition/ports"
)

type TagStep struct {
	tagger ports.AudioTagger
}

func NewTagStep(tagger ports.AudioTagger) *TagStep { return &TagStep{tagger: tagger} }

func (s *TagStep) Name() string { return "tag" }

// Execute writes the track's metadata into the downloaded file via the tagger
// port. Tagging failure is logged and swallowed — it must never fail the
// pipeline. Without a tagger wired the step is a no-op.
func (s *TagStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	if ac.TempPath == "" || s.tagger == nil {
		return nil
	}

	tags := ports.TrackTags{
		Title:       ac.Track.Title,
		Artist:      ac.Track.Artist,
		Album:       ac.Track.Album,
		AlbumArtist: ac.Track.AlbumArtist,
		Genre:       ac.Track.Genre,
		Year:        ac.Track.Year,
		TrackNumber: ac.Track.TrackNumber,
	}
	if err := s.tagger.Tag(ctx, ac.TempPath, tags); err != nil {
		slog.WarnContext(ctx, "tagging_failed", "track_id", ac.Track.ID, "error", err)
	}
	return nil
}

func (s *TagStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
