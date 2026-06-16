package domain

// VocabularyEntry is a known term in the music vocabulary, used for
// autocomplete suggestions and fuzzy query correction.
type VocabularyEntry struct {
	Term       string
	TermNorm   string
	Kind       string // "artist", "track", or "album"
	Popularity int64
}
