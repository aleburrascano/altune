package ytmsearch

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"log/slog"
	"math"
)

const SourceName domain.SourceName = "ytmsearch"

const topResults = 5

type songSearch interface {
	SearchSongs(ctx context.Context, query string) ([]Song, error)
}

type audioDownloader interface {
	Download(ctx context.Context, url string, outDir string) (domain.FetchedAudio, error)
}

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	search     songSearch
	downloader audioDownloader
}

func NewSource(search songSearch, downloader audioDownloader) *Source {
	return &Source{search: search, downloader: downloader}
}

func (s *Source) Name() domain.SourceName { return SourceName }

func (s *Source) Find(ctx context.Context, req domain.FindRequest) ([]domain.AudioCandidate, error) {
	if req.Title == "" || req.Artist == "" {
		return nil, nil
	}
	songs, err := s.search.SearchSongs(ctx, req.Title+" "+req.Artist)
	if err != nil {
		return nil, fmt.Errorf("ytmsearch songs: %w", err)
	}
	hit, ok := strongHit(songs, req)
	if !ok {
		slog.InfoContext(ctx, "acquisition.ytmsearch_no_strong_hit", "title", req.Title, "results", len(songs))
		return nil, nil
	}
	slog.InfoContext(ctx, "acquisition.ytmsearch_strong_hit",
		"video_id", hit.VideoID, "title", req.Title, "explicit", hit.Explicit)
	return []domain.AudioCandidate{CandidateFor(hit)}, nil
}

func CandidateFor(hit Song) domain.AudioCandidate {
	return domain.AudioCandidate{
		Title:    hit.Title,
		Duration: float64(hit.DurationSeconds),
		URL:      domain.YouTubeMusicWatchURL(hit.VideoID),
		Explicit: hit.Explicit,
		Artists:  hit.Artists,
		Album:    hit.Album,
		Kind:     domain.KindArtTrack,
	}
}

func (s *Source) Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	r0, err := s.downloader.Download(ctx, candidate.URL, outDir)
	if err != nil {
		return r0, fmt.Errorf("download audio: %w", err)
	}
	return r0, nil
}

func strongHit(songs []Song, req domain.FindRequest) (Song, bool) {
	var best Song
	found := false
	for _, song := range songs[:min(len(songs), topResults)] {
		if !domain.IsYouTubeVideoID(song.VideoID) || !matches(song, req) {
			continue
		}
		if !found || better(song, best, req) {
			best, found = song, true
		}
	}
	return best, found
}

func better(song, best Song, req domain.FindRequest) bool {
	if song.Explicit != best.Explicit {
		return song.Explicit == domain.PrefersExplicit(req.CatalogExplicit)
	}
	if onAlbum, bestOnAlbum := onCatalogAlbum(song, req), onCatalogAlbum(best, req); onAlbum != bestOnAlbum {
		return onAlbum
	}
	return req.Duration > 0 && lengthGap(song, req.Duration) < lengthGap(best, req.Duration)
}

func onCatalogAlbum(song Song, req domain.FindRequest) bool {
	return req.Album != "" && textnorm.NormalizeForMatch(song.Album) == textnorm.NormalizeForMatch(req.Album)
}

func lengthGap(song Song, savedLength float64) float64 {
	return math.Abs(float64(song.DurationSeconds) - savedLength)
}

func matches(song Song, req domain.FindRequest) bool {
	return song.Type == VideoTypeAudioOnly &&
		comparableTitle(song.Title) == comparableTitle(req.Title) &&
		domain.Credits(song.Artists, req.Artist)
}

func CreditsArtist(artists []string, artist string) bool { return domain.Credits(artists, artist) }

func comparableTitle(title string) string {
	return textnorm.NormalizeForIdentity(domain.CreditBrackets.ReplaceAllString(title, ""))
}
