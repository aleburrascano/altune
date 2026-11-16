package fixture

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	_ "embed"
	"math"
	"os"
	"path/filepath"
)

const (
	SourceName       acqdomain.SourceName = "fixture"
	UploadSourceName acqdomain.SourceName = "fixture-upload"
	ArtTrackURL                           = "https://fixture.invalid/art-track.mp3"
	UploadURL                             = "https://fixture.invalid/upload.mp3"
	uploadChannel                         = "Fixture Uploads"
	unknownLength                         = 180
	clipSeconds                           = 1.032
	maxClipRepeats                        = 1200
	fetchedName                           = "fixture.mp3"
)

//go:embed clip.mp3
var clip []byte

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	name acqdomain.SourceName
	emit func(ports.FindRequest) ports.AudioCandidate
}

func NewArtTrackSource() *Source {
	return &Source{name: SourceName, emit: artTrackCandidate}
}

func NewUploadSource() *Source {
	return &Source{name: UploadSourceName, emit: uploadCandidate}
}

func (s *Source) Name() acqdomain.SourceName { return s.name }

func (s *Source) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{s.emit(req)}, nil
}

func artTrackCandidate(req ports.FindRequest) ports.AudioCandidate {
	candidate := ytmsearch.CandidateFor(ytmsearch.Song{
		Type:            ytmsearch.VideoTypeATV,
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
		return ports.FetchedAudio{}, err
	}
	path := filepath.Join(outDir, fetchedName)
	audio := bytes.Repeat(clip, clipRepeats(candidate.Duration))
	if err := os.WriteFile(path, audio, 0o600); err != nil {
		return ports.FetchedAudio{}, err
	}
	return ports.FetchedAudio{Path: path}, nil
}

func clipRepeats(seconds float64) int {
	if !(seconds > clipSeconds) {
		return 1
	}
	return int(math.Min(math.Ceil(seconds/clipSeconds), maxClipRepeats))
}
