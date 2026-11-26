package search

import vocabulary "altune/go-api/internal/search/internal/service/vocabulary"

type (
	SuggestService              = vocabulary.SuggestService
	SpellingHintsRefreshService = vocabulary.SpellingHintsRefreshService
)

var (
	NewCorrectionService              = vocabulary.NewCorrectionService
	NewVocabularyIngestor             = vocabulary.NewVocabularyIngestor
	ErrVocabularyCollaboratorRequired = vocabulary.ErrCollaboratorRequired
	NewSuggestService                 = vocabulary.NewSuggestService
	NewVocabularyRefreshService       = vocabulary.NewVocabularyRefreshService
)

type CorrectionResult = vocabulary.CorrectionResult
