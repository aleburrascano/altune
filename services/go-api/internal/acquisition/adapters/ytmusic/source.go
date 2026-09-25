package ytmusic

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
	"regexp"
)

const (
	SourceName     = "ytmusic"
	identityKey    = ports.ProviderYouTube
	watchURLPrefix = "https://music.youtube.com/watch?v="
	catalogChannel = "YouTube Music catalog"
)

// videoIDPattern is YouTube's watch-id shape. The id arrives as third-party
// discovery data and is concatenated onto watchURLPrefix, so any other shape
// could steer the download off the watch endpoint entirely.
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

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
	if !videoIDPattern.MatchString(source.ExternalID) {
		slog.WarnContext(ctx, "acquisition.ytmusic_video_id_rejected",
			"video_id", source.ExternalID, "title", req.Title)
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
