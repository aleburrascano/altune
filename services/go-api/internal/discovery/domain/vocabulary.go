package domain

import "unicode/utf8"

const MaxVocabularyTermRunes = 200

func IsIndexableVocabularyTerm(term, norm string) bool {
	return utf8.RuneCountInString(term) <= MaxVocabularyTermRunes &&
		utf8.RuneCountInString(norm) <= MaxVocabularyTermRunes
}

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
