package discoverytest

import "altune/go-api/internal/discovery/domain"

func WithMBID(r domain.SearchResult, mbid string) domain.SearchResult {
	switch r.Kind {
	case domain.ResultKindTrack:
		r.RecordingMBID = mbid
	case domain.ResultKindAlbum:
		r.ReleaseGroupMBID = mbid
	case domain.ResultKindArtist:
		r.ArtistMBID = mbid
	}
	return r
}
