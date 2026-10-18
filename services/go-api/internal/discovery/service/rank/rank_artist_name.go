package rank

import "altune/go-api/internal/discovery/domain"

const strongArtistProviders = 3

func isStrongNamedArtist(r domain.SearchResult, queryNorm string) bool {
	if r.Kind != domain.ResultKindArtist || queryNorm == "" || matchName(r.Title) != matchName(queryNorm) {
		return false
	}
	return r.ArtistMBID != "" || len(providersOf(r)) >= strongArtistProviders
}
