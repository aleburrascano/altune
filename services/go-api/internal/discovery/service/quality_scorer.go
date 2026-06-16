package service

import (
	"math"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

var tierScores = map[string]float64{
	"mbid": 1.0,
	"isrc": 0.8,
	"none": 0.2,
}

const maxProviders = 6

var canonicalRecordTypes = map[string]bool{
	"album":  true,
	"single": true,
	"ep":     true,
}

func qualityCompleteness(r domain.SearchResult) float64 {
	fields := 0
	total := 4
	if getStringExtra(r, "isrc") != "" {
		fields++
	}
	if r.ImageURL != "" {
		fields++
	}
	if r.Extras != nil {
		if _, ok := r.Extras["duration"]; ok {
			fields++
		}
	}
	if getStringExtra(r, "album") != "" {
		fields++
	}
	return float64(fields) / float64(total)
}

func qualityAgreement(r domain.SearchResult) float64 {
	providers := providersOf(r)
	return math.Min(float64(len(providers))/float64(maxProviders), 1.0)
}

func entityTierSignal(r domain.SearchResult) float64 {
	tierVal := getStringExtra(r, "resolution_tier")
	if score, ok := tierScores[tierVal]; ok {
		return score
	}
	return tierScores["none"]
}

func ComputeQualityScore(r domain.SearchResult, fetchSuccessRate float64) domain.QualityScore {
	comp := qualityCompleteness(r)
	agr := qualityAgreement(r)
	tier := entityTierSignal(r)
	fs := math.Max(0.0, math.Min(1.0, fetchSuccessRate))
	composite := (comp + agr + tier + fs) / 4.0

	return domain.QualityScore{
		Completeness: composite,
		Agreement:    agr,
		EntityTier:   domain.EntityResolutionNone,
		FetchSuccess: fs,
	}
}

// IsDemoted returns true if the result's record_type is not canonical
// (album, single, ep). No record_type = not demoted.
func IsDemoted(r domain.SearchResult) bool {
	recordType := getStringExtra(r, "record_type")
	if recordType == "" {
		return false
	}
	return !canonicalRecordTypes[strings.ToLower(recordType)]
}
