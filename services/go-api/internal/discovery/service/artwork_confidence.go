package service

import "altune/go-api/internal/discovery/domain"

// Artwork confidence grades how much a bound cover can be trusted when two
// candidates for the same entity are merged. It replaces the old
// "first non-empty wins" rule, which let a provisional name-guess cover beat a
// real identity-pinned one (and blank out when the guess was skipped). The
// ordering mirrors ports.ArtworkConfidence (Identity > Name/provider > None).
const (
	artworkRankNone     = iota // no cover at all
	artworkRankProvider        // the entity's own provider image, or a bound name cover
	artworkRankIdentity        // an id-pinned cover (or a raw image on an id-bearing result)
)

// artworkConfidenceRank grades r's currently bound cover. A cover already graded
// by fillArtwork carries its taxonomy in the artwork_path extra; a raw
// pre-merge image is trusted as identity-grade only when the result itself
// carries a strong identity.
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

// hasArtworkIdentity reports whether r is pinned to a strong identity, so a
// cover riding on it is trustworthy rather than a bare name match.
func hasArtworkIdentity(r domain.SearchResult) bool {
	return r.MBID != "" || r.ISRC != "" || r.UPC != "" || len(r.Xref) > 0
}

// firstNonEmptyArtwork keeps the pre-existing coverage rule: the first cover
// that is present wins, carrying its source tag along.
func firstNonEmptyArtwork(a, b domain.SearchResult) (url, source string) {
	if a.ImageURL != "" {
		return a.ImageURL, a.ArtworkSource
	}
	return b.ImageURL, b.ArtworkSource
}

// mergedArtwork picks the cover to keep when two results for the same entity are
// merged. On a name-only match (look-alikes that merely share a title) a cover
// may only ride in on a strong identity: the side that carries an identity wins
// outright, so a clean placeholder beats a look-alike's wrong art. When the two
// are the same entity (a strong-id tier) either cover is the entity's own, so the
// higher-confidence one wins and the first non-empty one fills any gap.
func mergedArtwork(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) (url, source string) {
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
