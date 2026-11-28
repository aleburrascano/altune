package fakesources

import (
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	SourceName       domain.SourceName = "fixture"
	UploadSourceName domain.SourceName = "fixture-upload"
	ArtTrackURL                        = "https://fixture.invalid/art-track.mp3"
	UploadURL                          = "https://fixture.invalid/upload.mp3"
	uploadChannel                      = "Fixture Uploads"
	unknownLength                      = 180
	clipSeconds                        = 1.032
	maxClipRepeats                     = 1200
	fetchedName                        = "fixture.mp3"
)

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	name domain.SourceName
	emit func(ports.FindRequest) ports.AudioCandidate
	clip []byte
}

func NewArtTrackSource(clip []byte) *Source {
	return &Source{name: SourceName, emit: artTrackCandidate, clip: clip}
}

func NewUploadSource(clip []byte) *Source {
	return &Source{name: UploadSourceName, emit: uploadCandidate, clip: clip}
}

func (s *Source) Name() domain.SourceName { return s.name }

func (s *Source) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{s.emit(req)}, nil
}

func artTrackCandidate(req ports.FindRequest) ports.AudioCandidate {
	candidate := ytmsearch.CandidateFor(ytmsearch.Song{
		Type:            ytmsearch.VideoTypeAudioOnly,
		Title:           req.Title,
		Artists:         []string{req.Artist},
		Album:           req.Album,
		DurationSeconds: fixtureLength(req.Duration),
	})
	candidate.URL = ArtTrackURL
	return candidate
}

func uploadCandidate(req ports.FindRequest) ports.AudioCandidate {
	return ytdlp.CandidateFromEntry(ytdlp.Entry{
		Title:      req.Title,
		Duration:   float64(fixtureLength(req.Duration)),
		WebpageURL: UploadURL,
		Channel:    uploadChannel,
	})
}

func fixtureLength(seconds float64) int {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return unknownLength
	}
	return int(math.Round(seconds))
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if err := ctx.Err(); err != nil {
		return ports.FetchedAudio{}, fmt.Errorf("fixture fetch: %w", err)
	}
	path := filepath.Join(outDir, fetchedName)
	audio := bytes.Repeat(s.clip, clipRepeats(candidate.Duration))
	if err := os.WriteFile(path, audio, 0o600); err != nil {
		return ports.FetchedAudio{}, fmt.Errorf("write the fixture clip: %w", err)
	}
	return ports.FetchedAudio{Path: path}, nil
}

func clipRepeats(seconds float64) int {
	if !(seconds > clipSeconds) {
		return 1
	}
	return int(math.Min(math.Ceil(seconds/clipSeconds), maxClipRepeats))
}
