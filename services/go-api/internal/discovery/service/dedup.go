package service

import (
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

// FuseAndRank performs identifier-only merge (ISRC/MBID) and RRF ranking.
func FuseAndRank(providerResults []domain.ProviderSearchResponse, queryNorm string, limit int) []domain.SearchResult {
	groups := mergeByIdentifier(providerResults)
	ranked := rankResults(groups, queryNorm)

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// Rerank re-sorts results after enrichment changes popularity scores.
// Uses a different sort key from FuseAndRank: omits quality_score.
func Rerank(results []domain.SearchResult, queryNorm string) []domain.SearchResult {
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := results[i], results[j]

		relI := relevanceScore(ri, queryNorm)
		relJ := relevanceScore(rj, queryNorm)
		if relI != relJ {
			return relI > relJ
		}

		popI := getPopularity(ri)
		popJ := getPopularity(rj)
		return popI > popJ
	})
	return results
}

type mergeGroup struct {
	result    domain.SearchResult
	positions map[domain.ProviderName]int
}

func mergeByIdentifier(providerResults []domain.ProviderSearchResponse) []mergeGroup {
	isrcIndex := make(map[string]*mergeGroup)
	mbidIndex := make(map[string]*mergeGroup)
	var groups []mergeGroup

	for _, pr := range providerResults {
		for pos, result := range pr.Results {
			isrc := getExtra(result, "isrc")
			mbid := getExtra(result, "mbid")

			var matched *mergeGroup

			if mbid != "" {
				if existing, ok := mbidIndex[mbid]; ok {
					matched = existing
				}
			}
			if matched == nil && isrc != "" {
				if existing, ok := isrcIndex[isrc]; ok {
					matched = existing
				}
			}

			if matched != nil {
				matched.result.Sources = append(matched.result.Sources, result.Sources...)
				matched.positions[pr.Provider] = pos

				if mbid != "" {
					matched.result.Quality.EntityTier = domain.EntityResolutionMBID
					matched.result.Confidence = domain.ConfidenceHigh
				} else if isrc != "" && matched.result.Quality.EntityTier < domain.EntityResolutionISRC {
					matched.result.Quality.EntityTier = domain.EntityResolutionISRC
					matched.result.Confidence = domain.ConfidenceHigh
				}

				if result.ImageURL != "" && matched.result.ImageURL == "" {
					matched.result.ImageURL = result.ImageURL
				}
				if result.Subtitle != "" && matched.result.Subtitle == "" {
					matched.result.Subtitle = result.Subtitle
				}
			} else {
				g := mergeGroup{
					result:    result,
					positions: map[domain.ProviderName]int{pr.Provider: pos},
				}
				groups = append(groups, g)
				idx := len(groups) - 1

				if mbid != "" {
					mbidIndex[mbid] = &groups[idx]
					groups[idx].result.Quality.EntityTier = domain.EntityResolutionMBID
				}
				if isrc != "" {
					isrcIndex[isrc] = &groups[idx]
					if groups[idx].result.Quality.EntityTier < domain.EntityResolutionISRC {
						groups[idx].result.Quality.EntityTier = domain.EntityResolutionISRC
					}
				}
			}
		}
	}

	return groups
}

func rankResults(groups []mergeGroup, queryNorm string) []domain.SearchResult {
	type scoredResult struct {
		result   domain.SearchResult
		score    float64
	}

	var scored []scoredResult
	for _, g := range groups {
		rel := relevanceScore(g.result, queryNorm)
		rrf := rrfScore(g.positions)
		quality := g.result.Quality.Completeness*0.3 + g.result.Quality.Agreement*0.3 + float64(g.result.Quality.EntityTier)*0.2 + g.result.Quality.FetchSuccess*0.2

		total := rel*0.4 + rrf*0.3 + quality*0.3

		if isDemoted(g.result) {
			total *= 0.5
		}

		scored = append(scored, scoredResult{result: g.result, score: total})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	results := make([]domain.SearchResult, len(scored))
	for i, s := range scored {
		results[i] = s.result
	}
	return results
}

func relevanceScore(result domain.SearchResult, queryNorm string) float64 {
	titleNorm := NormalizeForMatch(result.Title)

	if titleNorm == queryNorm {
		return 1.0
	}

	if strings.Contains(titleNorm, queryNorm) || strings.Contains(queryNorm, titleNorm) {
		return 0.8
	}

	return 0.5
}

func rrfScore(positions map[domain.ProviderName]int) float64 {
	const k = 60.0
	score := 0.0
	for _, pos := range positions {
		score += 1.0 / (k + float64(pos))
	}
	return score
}

func isDemoted(result domain.SearchResult) bool {
	if result.Kind == domain.ResultKindTrack {
		title := strings.ToLower(result.Title)
		demotionTerms := []string{"remix", "live", "karaoke", "instrumental", "cover", "acoustic version"}
		for _, term := range demotionTerms {
			if strings.Contains(title, term) {
				return true
			}
		}
	}
	return false
}

func getExtra(result domain.SearchResult, key string) string {
	if result.Extras == nil {
		return ""
	}
	v, ok := result.Extras[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func getPopularity(result domain.SearchResult) int64 {
	if result.Extras == nil {
		return 0
	}
	v, ok := result.Extras["popularity"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}
