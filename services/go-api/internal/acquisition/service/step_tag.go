package service

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/bogem/id3v2/v2"
)

type TagStep struct{}

func NewTagStep() *TagStep { return &TagStep{} }

func (s *TagStep) Name() string { return "tag" }

func (s *TagStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	if ac.TempPath == "" {
		return nil
	}

	tag, err := id3v2.Open(ac.TempPath, id3v2.Options{Parse: false})
	if err != nil {
		slog.WarnContext(ctx, "id3_tagging_failed: could not open file", "error", err)
		return nil
	}
	defer tag.Close()

	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetVersion(4)

	tag.SetTitle(ac.Track.Title)
	tag.SetArtist(ac.Track.Artist)

	if ac.Track.Album != "" {
		tag.SetAlbum(ac.Track.Album)
	}
	if ac.Track.Year > 0 {
		tag.AddTextFrame(tag.CommonID("Year"), id3v2.EncodingUTF8, strconv.Itoa(ac.Track.Year))
	}
	if ac.Track.TrackNumber > 0 {
		tag.AddTextFrame(tag.CommonID("Track number/Position in set"), id3v2.EncodingUTF8, strconv.Itoa(ac.Track.TrackNumber))
	}
	if ac.Track.AlbumArtist != "" {
		tag.AddTextFrame(tag.CommonID("Band/Orchestra/Accompaniment"), id3v2.EncodingUTF8, ac.Track.AlbumArtist)
	}
	if ac.Track.Genre != "" {
		tag.SetGenre(ac.Track.Genre)
	}

	if err := tag.Save(); err != nil {
		slog.WarnContext(ctx, "id3_tagging_failed: could not save tags", "error", err)
		return nil
	}

	slog.InfoContext(ctx, "id3_tags_written", "track_id", ac.Track.ID)
	return nil
}

func (s *TagStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
