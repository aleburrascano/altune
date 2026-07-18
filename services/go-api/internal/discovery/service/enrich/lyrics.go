package enrich

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// LyricsService is the detail-open lyrics use case: resolve a track's Deezer id
// from (title, artist), then fetch its synced + plain lyrics — read-through
// cached by the normalized name key. Off the ranking path (display-only). Lyrics
// apply to tracks only (docs/providers/deezer.md cap 6).
//
// All external calls are best-effort: a resolve/lookup failure degrades to empty
// lyrics and a nil error (the endpoint always answers 200), never a surfaced
// error. A definitive miss (no track id, or no lyrics for this region) is
// negative-cached so the reverse-engineered pipe path is not re-hit every open.
type LyricsService struct {
	provider ports.LyricsProvider
	cache    ports.LyricsCache
}

// NewLyricsService wires the provider (required) with an optional cache (nil
// tolerated — runs uncached).
func NewLyricsService(provider ports.LyricsProvider, cache ports.LyricsCache) *LyricsService {
	return &LyricsService{provider: provider, cache: cache}
}

// Execute returns the lyrics for one track. The wire (title, subtitle) maps to
// Deezer's (title, artist): the subtitle is the artist, the title is the track.
// A cache hit short-circuits the network; a negatively-cached or unresolved
// track returns empty.
func (s *LyricsService) Execute(ctx context.Context, title, subtitle string) (domain.DeezerLyrics, error) {
	artist := strings.TrimSpace(subtitle)
	track := strings.TrimSpace(title)
	if s.provider == nil || track == "" {
		return domain.EmptyDeezerLyrics(), nil
	}

	return ports.CachedLookup(ctx, s.cache, lyricsNameKey(artist, track), domain.EmptyDeezerLyrics(),
		func(ctx context.Context) (domain.DeezerLyrics, bool, error) {
			trackID, err := s.provider.ResolveTrackID(ctx, artist, track)
			if err != nil {
				slog.WarnContext(ctx, "lyrics.resolve_failed",
					"artist", artist, "title", track, "error", err)
				return domain.EmptyDeezerLyrics(), false, err // transient; not cached negative
			}
			if trackID == "" {
				return domain.EmptyDeezerLyrics(), false, nil
			}
			l, err := s.provider.Lookup(ctx, trackID)
			if err != nil {
				slog.WarnContext(ctx, "lyrics.lookup_failed",
					"track_id", trackID, "title", track, "error", err)
				return domain.EmptyDeezerLyrics(), false, err // best-effort; don't poison the cache
			}
			if l.IsZero() {
				return domain.EmptyDeezerLyrics(), false, nil
			}
			return l, true, nil
		})
}

// lyricsNameKey is the normalized cache key for an (artist, title) lookup, pinned
// in the service so the key the cache hashes is consistent.
func lyricsNameKey(artist, title string) string {
	return textnorm.NormalizeForMatch("track " + artist + " " + title)
}
