package domain

import "unicode/utf8"

// MaxVocabularyTermRunes caps the term, and its normalized form, that the
// shared vocabulary index stores. Every user's suggest and correction lookups
// read that index, so no single writer may persist an oversized term in it.
const MaxVocabularyTermRunes = 200

// IsIndexableVocabularyTerm reports whether a term and its normalized form fit
// the shared vocabulary index bounds.
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
