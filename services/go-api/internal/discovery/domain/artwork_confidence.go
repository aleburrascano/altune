package domain

const (
	ArtworkRankNone = iota
	ArtworkRankProvider
	ArtworkRankIdentity
)

func (r SearchResult) ArtworkConfidenceRank() int {
	if r.ImageURL == "" {
		return ArtworkRankNone
	}
	switch r.ArtworkPath {
	case ArtworkPathDurableIdentity, ArtworkPathIdentity:
		return ArtworkRankIdentity
	case ArtworkPathName:
		return ArtworkRankProvider
	}
	if r.HasArtworkIdentity() {
		return ArtworkRankIdentity
	}
	return ArtworkRankProvider
}

func (r SearchResult) HasArtworkIdentity() bool {
	return r.MBID != "" || r.ISRC != "" || r.UPC != "" || len(r.Xref) > 0
}

func FirstNonEmptyArtwork(a, b SearchResult) (url string, source ProviderKey) {
	if a.ImageURL != "" {
		return a.ImageURL, a.ArtworkSource
	}
	return b.ImageURL, b.ArtworkSource
}
