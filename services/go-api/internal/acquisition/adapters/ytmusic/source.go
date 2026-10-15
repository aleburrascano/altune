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
	videoID, fromCatalogPin := pinnedVideoID(req)
	if videoID == "" {
		return nil, nil
	}
	if !videoIDPattern.MatchString(videoID) {
		slog.WarnContext(ctx, "acquisition.ytmusic_video_id_rejected",
			"video_id", videoID, "title", req.Title)
		return nil, nil
	}

	slog.InfoContext(ctx, "acquisition.ytmusic_resolved",
		"video_id", videoID, "title", req.Title)

	return []ports.AudioCandidate{s.pinnedCandidate(ctx, videoID, fromCatalogPin)}, nil
}

func pinnedVideoID(req ports.FindRequest) (videoID string, fromCatalogPin bool) {
	for _, pin := range req.CatalogSources {
		if pin.Provider == string(identityKey) && pin.ExternalID != "" {
			return pin.ExternalID, true
		}
	}
	resolved, _ := req.Identity.SourceFor(identityKey)
	return resolved.ExternalID, false
}

func (s *Source) pinnedCandidate(ctx context.Context, videoID string, fromCatalogPin bool) ports.AudioCandidate {
	return CandidateForWatch(videoID, s.observe(ctx, videoID), fromCatalogPin)
}

func CandidateForWatch(videoID string, observed ytmsearch.Watch, pinned bool) ports.AudioCandidate {
	return ports.AudioCandidate{
		Title:      observed.Title,
		Artists:    observed.Artists,
		Explicit:   observed.Explicit,
		URL:        watchURLPrefix + videoID,
		Categories: []string{"Music"},
		Resolved:   pinned || isCatalogVideo(observed.Type),
		Kind:       kindOf(observed.Type),
	}
}

func isCatalogVideo(videoType ytmsearch.VideoType) bool {
	return videoType == ytmsearch.VideoTypeATV || videoType == ytmsearch.VideoTypeOMV
}

func kindOf(videoType ytmsearch.VideoType) ports.CandidateKind {
	switch videoType {
	case ytmsearch.VideoTypeATV:
		return ports.KindArtTrack
	case ytmsearch.VideoTypeUGC:
		return ports.KindUpload
	default:
		return ports.KindPinnedVideo
	}
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
