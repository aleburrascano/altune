package service

import "altune/go-api/internal/discovery/domain"

func mergedArtwork(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) (url string, source domain.ProviderKey) {
	if tier == domain.EntityResolutionNone {
		switch {
		case canonical.HasArtworkIdentity() && !other.HasArtworkIdentity():
			return canonical.ImageURL, canonical.ArtworkSource
		case other.HasArtworkIdentity() && !canonical.HasArtworkIdentity():
			return other.ImageURL, other.ArtworkSource
		}
	}
	if other.ArtworkConfidenceRank() > canonical.ArtworkConfidenceRank() {
		return other.ImageURL, other.ArtworkSource
	}
	return domain.FirstNonEmptyArtwork(canonical, other)
}
