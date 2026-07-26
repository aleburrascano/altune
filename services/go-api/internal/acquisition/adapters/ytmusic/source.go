package ytmusic

import (
	"context"
	"log/slog"

	"altune/go-api/internal/acquisition/ports"
)

const (
	SourceName     = "ytmusic"
	identityKey    = "youtube"
	watchURLPrefix = "https://music.youtube.com/watch?v="
	catalogChannel = "YouTube Music catalog"
)

type audioFetcher interface {
	Download(ctx context.Context, url string, outDir string) (string, error)
}

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	fetcher audioFetcher
}

func NewSource(fetcher audioFetcher) *Source {
	return &Source{fetcher: fetcher}
}

func (s *Source) Name() string { return SourceName }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	source, ok := req.Identity.SourceFor(identityKey)
	if !ok || source.ExternalID == "" {
		return nil, nil
	}

	slog.InfoContext(ctx, "acquisition.ytmusic_resolved",
		"video_id", source.ExternalID, "title", req.Title)

	return []ports.AudioCandidate{{
		Title:      req.Title,
		Duration:   req.Identity.Duration,
		URL:        watchURLPrefix + source.ExternalID,
		Channel:    catalogChannel,
		Categories: []string{"Music"},
		Resolved:   true,
	}}, nil
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (string, error) {
	return s.fetcher.Download(ctx, candidate.URL, outDir)
}
