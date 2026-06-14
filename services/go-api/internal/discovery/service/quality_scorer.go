package service

import (
	"altune/go-api/internal/discovery/domain"
)

type QualityScorer struct{}

func NewQualityScorer() *QualityScorer {
	return &QualityScorer{}
}

func (qs *QualityScorer) Score(result *domain.SearchResult, fetchSuccessRate float64) domain.QualityScore {
	completeness := computeCompleteness(result)
	agreement := computeAgreement(result)

	tier := domain.EntityResolutionNone
	if result.Quality.EntityTier > tier {
		tier = result.Quality.EntityTier
	}

	return domain.QualityScore{
		Completeness: completeness,
		Agreement:    agreement,
		EntityTier:   tier,
		FetchSuccess: fetchSuccessRate,
	}
}

func computeCompleteness(result *domain.SearchResult) float64 {
	fields := 0
	total := 4

	if result.Title != "" {
		fields++
	}
	if result.Subtitle != "" {
		fields++
	}
	if result.ImageURL != "" {
		fields++
	}
	if len(result.Sources) > 0 {
		fields++
	}

	return float64(fields) / float64(total)
}

func computeAgreement(result *domain.SearchResult) float64 {
	sourceCount := len(result.Sources)
	if sourceCount <= 1 {
		return 0.0
	}
	if sourceCount >= 3 {
		return 1.0
	}
	return 0.5
}
