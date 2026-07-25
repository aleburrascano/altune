package id3

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"altune/go-api/internal/acquisition/ports"

	"github.com/bogem/id3v2/v2"
)

type Tagger struct{}

func NewTagger() *Tagger { return &Tagger{} }

func (t *Tagger) Tag(ctx context.Context, filePath string, tags ports.TrackTags) error {
	if !strings.HasSuffix(strings.ToLower(filePath), ".mp3") {
		slog.InfoContext(ctx, "tag_skipped_non_mp3", "path", filePath)
		return nil
	}

	tag, err := id3v2.Open(filePath, id3v2.Options{Parse: false})
	if err != nil {
		return fmt.Errorf("open for tagging: %w", err)
	}
	defer tag.Close()

	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetVersion(4)

	tag.SetTitle(tags.Title)
	tag.SetArtist(tags.Artist)

	if tags.Album != "" {
		tag.SetAlbum(tags.Album)
	}
	if tags.Year > 0 {
		tag.AddTextFrame(tag.CommonID("Year"), id3v2.EncodingUTF8, strconv.Itoa(tags.Year))
	}
	if tags.TrackNumber > 0 {
		tag.AddTextFrame(tag.CommonID("Track number/Position in set"), id3v2.EncodingUTF8, strconv.Itoa(tags.TrackNumber))
	}
	if tags.AlbumArtist != "" {
		tag.AddTextFrame(tag.CommonID("Band/Orchestra/Accompaniment"), id3v2.EncodingUTF8, tags.AlbumArtist)
	}
	if tags.Genre != "" {
		tag.SetGenre(tags.Genre)
	}

	if err := tag.Save(); err != nil {
		return fmt.Errorf("save tags: %w", err)
	}

	slog.InfoContext(ctx, "id3_tags_written", "path", filePath)
	return nil
}
