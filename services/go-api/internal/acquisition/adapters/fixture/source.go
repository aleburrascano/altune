package fixture

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	_ "embed"
	"math"
	"os"
	"path/filepath"
)

const (
	SourceName     = "fixture"
	clipSeconds    = 1.032
	maxClipRepeats = 1200
	candidateURL   = "https://fixture.invalid/clip.mp3"
	fetchedName    = "fixture.mp3"
)

//go:embed clip.mp3
var clip []byte

var _ ports.AudioSource = (*Source)(nil)

type Source struct{}

func NewSource() *Source { return &Source{} }

func (s *Source) Name() string { return SourceName }

func (s *Source) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title:    req.Title,
		Channel:  req.Artist,
		Duration: req.Duration,
		URL:      candidateURL,
	}}, nil
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, fetchedName)
	audio := bytes.Repeat(clip, clipRepeats(candidate.Duration))
	if err := os.WriteFile(path, audio, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func clipRepeats(seconds float64) int {
	if !(seconds > clipSeconds) {
		return 1
	}
	return int(math.Min(math.Ceil(seconds/clipSeconds), maxClipRepeats))
}
