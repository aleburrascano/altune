package domain

// MBEnrichment is the MusicBrainz-derived detail-screen enrichment for one
// entity: curated genres, year, community rating, release types, the
// cross-provider id bridge, and a resolved HD artwork URL. It is an immutable
// value object — a live read surface fetched on detail-open, never persisted.
//
// Introduced by docs/specs/musicbrainz-enrichment/spec.md.
type MBEnrichment struct {
	MBID           string
	Genres         []string          // curated, deduped, vote-count desc (ties alphabetical)
	Year           int               // first-release-date year; 0 when absent/malformed/artist
	Rating         float64           // community rating 0–5; 0 when none
	RatingVotes    int               // vote count behind Rating
	PrimaryType    string            // album only (Album/EP/Single); "" for artist
	SecondaryTypes []string          // album only (Compilation/Live/…); empty otherwise
	ExternalIDs    map[string]string // lowercase provider name → bare id (deezer/spotify/discogs/wikidata)
	ArtworkURL     string            // HD cover via the artwork chain; "" on miss
}

// EmptyEnrichment returns a zero-value enrichment with non-nil collections, so
// callers and the wire mapping never see null genres/types/ids. The graceful
// degradation path (unresolved MBID, MB error) returns this.
func EmptyEnrichment() MBEnrichment {
	return MBEnrichment{
		Genres:         []string{},
		SecondaryTypes: []string{},
		ExternalIDs:    map[string]string{},
	}
}

// IsZero reports whether the enrichment carries no resolved entity — used to
// decide there is nothing to render. An enrichment with an MBID is never zero,
// even if every other field is empty (the MBID alone unlocks artwork).
func (e MBEnrichment) IsZero() bool {
	return e.MBID == "" &&
		len(e.Genres) == 0 &&
		e.Year == 0 &&
		e.Rating == 0 &&
		e.PrimaryType == "" &&
		len(e.SecondaryTypes) == 0 &&
		len(e.ExternalIDs) == 0 &&
		e.ArtworkURL == ""
}
