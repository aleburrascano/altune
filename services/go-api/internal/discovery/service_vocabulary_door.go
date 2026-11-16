package discovery

import vocabulary "altune/go-api/internal/discovery/service/vocabulary"

type (
	SuggestService           = vocabulary.SuggestService
	VocabularyRefreshService = vocabulary.VocabularyRefreshService
)

var (
	NewCorrectionService        = vocabulary.NewCorrectionService
	NewSuggestService           = vocabulary.NewSuggestService
	NewVocabularyRefreshService = vocabulary.NewVocabularyRefreshService
)

type CorrectionResult = vocabulary.CorrectionResult
