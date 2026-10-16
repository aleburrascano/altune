package ffmpegtag

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/execcmd"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Tagger struct {
	ffmpeg   string
	fallback ports.AudioTagger
}

func NewTagger(ffmpegBinary string, fallback ports.AudioTagger) *Tagger {
	return &Tagger{ffmpeg: ffmpegBinary, fallback: fallback}
}

func (t *Tagger) Tag(ctx context.Context, filePath string, tags ports.TrackTags) error {
	if !strings.HasSuffix(strings.ToLower(filePath), ".m4a") {
		return t.fallback.Tag(ctx, filePath, tags)
	}

	tagged := filePath + ".tagging.m4a"
	args := []string{"-v", "error", "-y", "-i", filePath, "-map", "0:a", "-c", "copy", "-map_metadata", "-1"}
	args = append(args, metadataArgs(tags)...)
	args = append(args, "-movflags", "+faststart", tagged)

	if _, stderr, err := execcmd.Run(ctx, t.ffmpeg, args...); err != nil {
		_ = os.Remove(tagged)
		return fmt.Errorf("ffmpeg tag: %w (stderr: %s)", err, strings.TrimSpace(stderr))
	}
	if tags.ISRC != "" {
		if err := embedISRC(tagged, tags.ISRC); err != nil {
			_ = os.Remove(tagged)
			return fmt.Errorf("embed isrc: %w", err)
		}
	}
	if err := os.Rename(tagged, filePath); err != nil {
		_ = os.Remove(tagged)
		return fmt.Errorf("replace tagged file: %w", err)
	}

	slog.InfoContext(ctx, "m4a_tags_written", "path", filePath)
	return nil
}

func metadataArgs(tags ports.TrackTags) []string {
	fields := [][2]string{{"title", tags.Title}, {"artist", tags.Artist}, {"album", tags.Album}, {"album_artist", tags.AlbumArtist}, {"genre", tags.Genre}}
	if tags.Year > 0 {
		fields = append(fields, [2]string{"date", strconv.Itoa(tags.Year)})
	}
	if tags.TrackNumber > 0 {
		fields = append(fields, [2]string{"track", strconv.Itoa(tags.TrackNumber)})
	}
	var args []string
	for _, field := range fields {
		if field[1] != "" {
			args = append(args, "-metadata", field[0]+"="+field[1])
		}
	}
	return args
}
