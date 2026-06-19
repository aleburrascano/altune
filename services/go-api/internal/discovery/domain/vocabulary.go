package domain

// VocabularyKind classifies a vocabulary entry.
type VocabularyKind string

const (
	VocabKindArtist VocabularyKind = "artist"
	VocabKindTrack  VocabularyKind = "track"
	VocabKindAlbum  VocabularyKind = "album"
	VocabKindQuery  VocabularyKind = "query"
)

// VocabularyEntry is a known term in the music vocabulary, used for
// autocomplete suggestions and fuzzy query correction.
type VocabularyEntry struct {
	Term       string
	TermNorm   string
	Kind       VocabularyKind
	Popularity int64
	MatchScore float64 // populated by FindClosest: combined Jaccard+phonetic score
}
