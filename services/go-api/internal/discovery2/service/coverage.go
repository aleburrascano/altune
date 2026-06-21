package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

// Layer 1 coverage fallbacks for the detail surface.
//
// Two of the three coverage gaps are content holes on the detail screen: a
// long-tail album whose primary provider has no tracklist, and an underground
// artist whose primary provider returns no top tracks. Rather than show an empty
// screen, fall back to the next provider. (The third gap — the YouTube Music
// 0-results bug — is fixed in the shared adapter, the Pattern-C cause.)

// TrackFetcher fetches a content tracklist from one provider (an album's tracks
// or an artist's top tracks).
type TrackFetcher func(ctx context.Context) ([]domain.SearchResult, error)

// FirstNonEmptyTracks returns the first provider's non-empty tracklist, trying
// each fetcher in order. A fetcher error is treated as empty and falls through
// to the next; the last error surfaces only if every provider yields nothing.
// Context cancellation short-circuits.
func FirstNonEmptyTracks(ctx context.Context, fetchers ...TrackFetcher) ([]domain.SearchResult, error) {
	var lastErr error
	for _, fetch := range fetchers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tracks, err := fetch(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if len(tracks) > 0 {
			return tracks, nil
		}
	}
	return nil, lastErr
}
