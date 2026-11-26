package details

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type LyricsService struct {
	provider ports.LyricsProvider
	cache    ports.LyricsCache
}

func NewLyricsService(provider ports.LyricsProvider, cache ports.LyricsCache) (*LyricsService, error) {
	for name, missing := range map[string]bool{
		"lyrics provider": provider == nil,
		"lyrics cache":    cache == nil,
	} {
		if missing {
			return nil, fmt.Errorf("%w: %s is nil", ErrCollaboratorRequired, name)
		}
	}
	return &LyricsService{provider: provider, cache: cache}, nil
}

func (s *LyricsService) Execute(ctx context.Context, title, subtitle string) (domain.DeezerLyrics, error) {
	artist := strings.TrimSpace(subtitle)
	track := strings.TrimSpace(title)
	if track == "" {
		return domain.EmptyDeezerLyrics(), nil
	}

	return CachedLookup(ctx, s.cache, kindNameKey(domain.ResultKindTrack, artist, track), domain.EmptyDeezerLyrics(),
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
