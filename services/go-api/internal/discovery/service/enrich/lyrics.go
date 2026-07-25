package enrich

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type LyricsService struct {
	provider ports.LyricsProvider
	cache    ports.LyricsCache
}

func NewLyricsService(provider ports.LyricsProvider, cache ports.LyricsCache) *LyricsService {
	return &LyricsService{provider: provider, cache: cache}
}

func (s *LyricsService) Execute(ctx context.Context, title, subtitle string) (domain.DeezerLyrics, error) {
	artist := strings.TrimSpace(subtitle)
	track := strings.TrimSpace(title)
	if s.provider == nil || track == "" {
		return domain.EmptyDeezerLyrics(), nil
	}

	return CachedLookup(ctx, s.cache, lyricsNameKey(artist, track), domain.EmptyDeezerLyrics(),
		func(ctx context.Context) (domain.DeezerLyrics, bool, error) {
			trackID, err := s.provider.ResolveTrackID(ctx, artist, track)
			if err != nil {
				slog.WarnContext(ctx, "lyrics.resolve_failed",
					"artist", artist, "title", track, "error", err)
				return domain.EmptyDeezerLyrics(), false, err
			}
			if trackID == "" {
				return domain.EmptyDeezerLyrics(), false, nil
			}
			l, err := s.provider.Lookup(ctx, trackID)
			if err != nil {
				slog.WarnContext(ctx, "lyrics.lookup_failed",
					"track_id", trackID, "title", track, "error", err)
				return domain.EmptyDeezerLyrics(), false, err
			}
			if l.IsZero() {
				return domain.EmptyDeezerLyrics(), false, nil
			}
			return l, true, nil
		})
}

func lyricsNameKey(artist, title string) string {
	return textnorm.NormalizeForMatch("track " + artist + " " + title)
}
