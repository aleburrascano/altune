package providers

import "altune/go-api/internal/discovery/domain"

// allSearchKinds returns a fresh map of the three result kinds supported by
// providers that cover tracks, albums, and artists. A new map is allocated on
// every call so callers can safely mutate the returned value.
func allSearchKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}
