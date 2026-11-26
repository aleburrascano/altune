package domain

import "unicode/utf8"

const MaxVocabularyTermRunes = 200

func IsIndexableVocabularyTerm(term, norm string) bool {
	return utf8.RuneCountInString(term) <= MaxVocabularyTermRunes &&
		utf8.RuneCountInString(norm) <= MaxVocabularyTermRunes
}

type SpellingHintsKind string

const (
	VocabKindArtist SpellingHintsKind = "artist"
	VocabKindTrack  SpellingHintsKind = "track"
	VocabKindAlbum  SpellingHintsKind = "album"
	VocabKindQuery  SpellingHintsKind = "query"
)

type SpellingHintsEntry struct {
	Term       string
	TermNorm   string
	Kind       SpellingHintsKind
	Popularity int64
	MatchScore float64
}

const (
	weightJaccard     = 0.35
	weightLevenshtein = 0.30
	weightPhonetic    = 0.20
	weightLengthSim   = 0.15
)

func SpellingHintsMatchScore(jaccard, levenshteinSim, phonetic, lengthSim float64) float64 {
	return weightJaccard*jaccard +
		weightLevenshtein*levenshteinSim +
		weightPhonetic*phonetic +
		weightLengthSim*lengthSim
}
