package youtubemusic

import (
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/sourcekey"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

const (
	SourceName  acqdomain.SourceName = "ytmusic"
	identityKey                      = ports.ProviderYouTube
)

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
	if !sourcekey.IsYouTubeVideoID(videoID) {
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
	resolved := pinned || isCatalogVideo(observed.Type)
	kind := ports.KindUpload
	if resolved {
		kind = resolvedKind(observed.Type)
	}
	return ports.AudioCandidate{
		Title:       observed.Title,
		Artists:     observed.Artists,
		Explicit:    observed.Explicit,
		URL:         sourcekey.YouTubeMusicWatchURL(videoID),
		Categories:  []string{"Music"},
		Kind:        kind,
		ObservedOMV: observed.Type == ytmsearch.VideoTypeOfficialVideo,
	}
}

func isCatalogVideo(videoType ytmsearch.VideoType) bool {
	return videoType == ytmsearch.VideoTypeAudioOnly || videoType == ytmsearch.VideoTypeOfficialVideo
}

func resolvedKind(videoType ytmsearch.VideoType) ports.CandidateKind {
	if videoType == ytmsearch.VideoTypeAudioOnly {
		return ports.KindArtTrack
	}
	return ports.KindPinnedVideo
}

func (s *Source) observe(ctx context.Context, videoID string) ytmsearch.Watch {
	watch, err := s.lookup.Watch(ctx, videoID)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.ytmusic_lookup_failed", "video_id", videoID, logging.KeyError, err)
		return ytmsearch.Watch{}
	}
	return watch
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	r0, err := s.fetcher.Download(ctx, candidate.URL, outDir)
	if err != nil {
		return r0, fmt.Errorf("download audio: %w", err)
	}
	return r0, nil
}
