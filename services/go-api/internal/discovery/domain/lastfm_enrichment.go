package domain

// LastFmEnrichment is the Last.fm-derived detail-screen enrichment for one
// entity (artist / track / album): the listen-based popularity, weighted
// folksonomy tags, biography/wiki prose, and similar-artist graph Last.fm
// carries and neither MusicBrainz nor Discogs do. Immutable value object — a
// live read surface fetched on detail-open, never persisted. Last.fm is the
// listening-behavior authority, so this complements MBEnrichment (identity +
// curated genres + artwork) and DiscogsEnrichment (credits + styles).
//
// One value object covers all three kinds; the kind-specific fields (Similar
// for artists, Duration/Album for tracks) are zero when not applicable.
//
// Introduced by docs/providers/lastfm.md (capability 3, with cap-4 similar
// artists folded into the artist payload).
type LastFmEnrichment struct {
	MBID      string   // the entity's MusicBrainz id — Last.fm's getInfo bridge back to MB; "" when absent
	Listeners int64    // distinct listeners (scrobble-based popularity)
	Playcount int64    // total plays (scrobble-based popularity)
	Tags      []string // weighted folksonomy tags, ordered by relevance
	Bio       string   // biography (artist) / blurb (track, album), HTML + "Read more" link stripped
	Similar   []string // similar artists (artist kind only — cap 4, folded in from artist.getInfo)
	Duration  int      // track only: length in seconds; 0 when absent
	Album     string   // track only: the album the track belongs to
}

// EmptyLastFmEnrichment returns a zero-value enrichment with non-nil
// collections, so the wire mapping never emits null lists. The graceful
// degradation path (unresolved entity, Last.fm error) returns this.
func EmptyLastFmEnrichment() LastFmEnrichment {
	return LastFmEnrichment{
		Tags:    []string{},
		Similar: []string{},
	}
}

// IsZero reports whether the enrichment carries nothing worth rendering — used
// to decide there is no section to show.
func (e LastFmEnrichment) IsZero() bool {
	return e.MBID == "" &&
		e.Listeners == 0 &&
		e.Playcount == 0 &&
		len(e.Tags) == 0 &&
		e.Bio == "" &&
		len(e.Similar) == 0 &&
		e.Duration == 0 &&
		e.Album == ""
}
