package domain

// DeezerEnrichment is the Deezer-derived detail-screen enrichment for one entity
// (track or album): the audio fields (BPM, ReplayGain), explicit flag, and album
// liner data (label, genres, barcode) Deezer's detail endpoints carry but the
// thin search projection drops. Immutable value object — a live read surface
// fetched on detail-open, never persisted. Complements MBEnrichment (identity +
// curated genres + artwork), DiscogsEnrichment (credits + styles), and
// LastFmEnrichment (listen popularity + tags).
//
// One value object covers both kinds; the kind-specific fields are zero when not
// applicable (track fills BPM/Gain/Explicit; album fills Label/Genres/UPC/
// RecordType). Introduced by docs/providers/deezer.md (caps 7–8). Lyrics (cap 6)
// are a separate feature and not modeled here.
type DeezerEnrichment struct {
	BPM        int      // track: beats per minute, rounded; 0 = unknown (Deezer reports 0 on many tracks)
	Gain       float64  // track: ReplayGain in dB; 0 = absent (a volume-normalization value, not a display field)
	Explicit   bool     // track: explicit lyrics flag
	Label      string   // album: record label
	Genres     []string // album: Deezer genre names
	UPC        string   // album: barcode (payload only — not user-facing)
	RecordType string   // album: "album" / "single" / "ep" / "compilation"
}

// EmptyDeezerEnrichment returns a zero-value enrichment with a non-nil Genres
// slice, so the wire mapping never emits a null list. The graceful degradation
// path (unresolved entity, Deezer error) returns this.
func EmptyDeezerEnrichment() DeezerEnrichment {
	return DeezerEnrichment{Genres: []string{}}
}

// IsZero reports whether the enrichment carries nothing worth rendering — used
// to decide there is no section to show.
func (e DeezerEnrichment) IsZero() bool {
	return e.BPM == 0 &&
		e.Gain == 0 &&
		!e.Explicit &&
		e.Label == "" &&
		len(e.Genres) == 0 &&
		e.UPC == "" &&
		e.RecordType == ""
}
