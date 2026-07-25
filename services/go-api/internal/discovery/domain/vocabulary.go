package domain

type VocabularyKind string

const (
	VocabKindArtist VocabularyKind = "artist"
	VocabKindTrack  VocabularyKind = "track"
	VocabKindAlbum  VocabularyKind = "album"
	VocabKindQuery  VocabularyKind = "query"
)

type VocabularyEntry struct {
	Term       string
	TermNorm   string
	Kind       VocabularyKind
	Popularity int64
	MatchScore float64
}
