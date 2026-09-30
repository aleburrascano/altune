package service

import "altune/go-api/internal/discovery/domain"

const (
	artworkRankNone = iota
	artworkRankProvider
	artworkRankIdentity
)

func artworkConfidenceRank(r domain.SearchResult) int {
	if r.ImageURL == "" {
		return artworkRankNone
	}
	switch stringExtra(r.Extras, domain.ExtraArtworkPath) {
	case "durable-identity", "identity":
		return artworkRankIdentity
	case "name":
		return artworkRankProvider
	}
	if hasArtworkIdentity(r) {
		return artworkRankIdentity
	}
	return artworkRankProvider
}

func hasArtworkIdentity(r domain.SearchResult) bool {
	return r.MBID != "" || r.ISRC != "" || r.UPC != "" || len(r.Xref) > 0
}

func firstNonEmptyArtwork(a, b domain.SearchResult) (url string, source domain.ProviderKey) {
	if a.ImageURL != "" {
		return a.ImageURL, a.ArtworkSource
	}
	return b.ImageURL, b.ArtworkSource
}

func mergedArtwork(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) (url string, source domain.ProviderKey) {
	if tier == domain.EntityResolutionNone {
		switch {
		case hasArtworkIdentity(canonical) && !hasArtworkIdentity(other):
			return canonical.ImageURL, canonical.ArtworkSource
		case hasArtworkIdentity(other) && !hasArtworkIdentity(canonical):
			return other.ImageURL, other.ArtworkSource
		}
	}
	if artworkConfidenceRank(other) > artworkConfidenceRank(canonical) {
		return other.ImageURL, other.ArtworkSource
	}
	return firstNonEmptyArtwork(canonical, other)
}
