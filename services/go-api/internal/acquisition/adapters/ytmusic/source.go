package ytmusic

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
	"regexp"
)

const (
	SourceName     acqdomain.SourceName = "ytmusic"
	identityKey                         = ports.ProviderYouTube
	watchURLPrefix                      = "https://music.youtube.com/watch?v="
	catalogChannel                      = "YouTube Music catalog"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type audioFetcher interface {
	Download(ctx context.Context, url string, outDir string) (ports.FetchedAudio, error)
}

type videoLookup interface {
	Watch(ctx context.Context, videoID string) (ytmsearch.Watch, error)
}

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	fetcher audioFetcher
	lookup  videoLookup
}

func NewSource(fetcher audioFetcher, lookup videoLookup) *Source {
	return &Source{fetcher: fetcher, lookup: lookup}
}

func (s *Source) Name() acqdomain.SourceName { return SourceName }

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

	observed := s.observe(ctx, source.ExternalID)

	return []ports.AudioCandidate{{
		Title:      observed.Title,
		Artists:    observed.Artists,
		Explicit:   observed.Explicit,
		URL:        watchURLPrefix + source.ExternalID,
		Channel:    catalogChannel,
		Categories: []string{"Music"},
		Resolved:   true,
		Kind:       ports.KindPinnedVideo,
	}}, nil
}

func (s *Source) observe(ctx context.Context, videoID string) ytmsearch.Watch {
	watch, err := s.lookup.Watch(ctx, videoID)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.ytmusic_lookup_failed", "video_id", videoID, "error", err)
		return ytmsearch.Watch{}
	}
	return watch
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	return s.fetcher.Download(ctx, candidate.URL, outDir)
}
