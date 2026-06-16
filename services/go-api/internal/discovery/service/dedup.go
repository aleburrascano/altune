package service

import (
	"math"
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

const rrfK = 60

func isrcOf(r domain.SearchResult) string {
	return getStringExtra(r, "isrc")
}

func mbidOf(r domain.SearchResult) string {
	return getStringExtra(r, "mbid")
}

func popularity(r domain.SearchResult) float64 {
	if r.Extras == nil {
		return 0.0
	}
	v, ok := r.Extras["popularity"]
	if !ok {
		return 0.0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0.0
	}
}

func completeness(r domain.SearchResult) int {
	count := 0
	if r.ImageURL != "" {
		count++
	}
	if getStringExtra(r, "isrc") != "" {
		count++
	}
	if r.Extras != nil {
		if _, ok := r.Extras["duration_seconds"]; ok {
			count++
		}
	}
	if getStringExtra(r, "album") != "" {
		count++
	}
	return count
}

func signature(r domain.SearchResult) string {
	title := NormalizeForMatch(r.Title)
	subtitle := NormalizeForMatch(r.Subtitle)
	return subtitle + "|" + title
}

func sharesWord(r domain.SearchResult, queryNorm string) bool {
	queryWords := contentWords(queryNorm)
	if len(queryWords) == 0 {
		raw := strings.Fields(strings.TrimSpace(queryNorm))
		text := NormalizeForMatch(r.Subtitle + " " + r.Title)
		textTokens := strings.Fields(text)
		for _, w := range raw {
			if w == "" {
				continue
			}
			for _, tw := range textTokens {
				if w == tw {
					return true
				}
			}
		}
		return false
	}
	text := NormalizeForMatch(r.Subtitle + " " + r.Title)
	textWords := contentWords(text)
	for w := range queryWords {
		if textWords[w] {
			return true
		}
	}
	return false
}

func contentWords(text string) map[string]bool {
	result := make(map[string]bool)
	for _, w := range strings.Fields(text) {
		if len(w) >= 2 {
			result[w] = true
		}
	}
	return result
}

func providersOf(r domain.SearchResult) map[domain.ProviderName]bool {
	m := make(map[domain.ProviderName]bool)
	for _, s := range r.Sources {
		m[s.Provider] = true
	}
	return m
}

func mergeResults(a, b domain.SearchResult, conf domain.Confidence, tier domain.EntityResolutionTier) domain.SearchResult {
	canonical, other := a, b
	if completeness(b) > completeness(a) {
		canonical, other = b, a
	}

	seen := make(map[string]bool)
	var sources []domain.SourceRef
	for _, s := range append(canonical.Sources, other.Sources...) {
		key := s.Provider.String() + ":" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, s)
	}

	extras := make(map[string]any)
	for k, v := range other.Extras {
		extras[k] = v
	}
	for k, v := range canonical.Extras {
		if v != nil || extras[k] == nil {
			extras[k] = v
		}
	}
	pop := math.Max(popularity(a), popularity(b))
	if pop > 0 {
		extras["popularity"] = pop
	}
	extras["resolution_tier"] = tier.String()

	imageURL := canonical.ImageURL
	if imageURL == "" {
		imageURL = other.ImageURL
	}

	return domain.SearchResult{
		Kind:       canonical.Kind,
		Title:      canonical.Title,
		Subtitle:   canonical.Subtitle,
		ImageURL:   imageURL,
		Confidence: conf,
		Sources:    sources,
		Extras:     extras,
	}
}

func tryMerge(a, b domain.SearchResult) (domain.SearchResult, bool) {
	if a.Kind != b.Kind {
		return domain.SearchResult{}, false
	}
	isrcA, isrcB := isrcOf(a), isrcOf(b)
	if isrcA != "" && isrcB != "" && isrcA == isrcB {
		return mergeResults(a, b, domain.ConfidenceHigh, domain.EntityResolutionISRC), true
	}
	mbidA, mbidB := mbidOf(a), mbidOf(b)
	if mbidA != "" && mbidB != "" {
		if mbidA == mbidB {
			return mergeResults(a, b, domain.ConfidenceHigh, domain.EntityResolutionMBID), true
		}
		return domain.SearchResult{}, false
	}
	// Name-based merge for artists: same normalized name = same artist.
	// Artists lack cross-provider identifiers (no ISRC, rarely MBID from
	// non-MB providers), so without this, 4+ copies of "The Weeknd" appear.
	if a.Kind == domain.ResultKindArtist {
		normA := NormalizeForMatch(a.Title)
		normB := NormalizeForMatch(b.Title)
		if normA != "" && normA == normB {
			return mergeResults(a, b, domain.ConfidenceMedium, domain.EntityResolutionNone), true
		}
	}
	return domain.SearchResult{}, false
}

func asLowConfidence(r domain.SearchResult) domain.SearchResult {
	if r.Confidence == domain.ConfidenceLow {
		return r
	}
	r.Confidence = domain.ConfidenceLow
	return r
}

func relevanceScore(result domain.SearchResult, queryNorm string) float64 {
	query := strings.TrimSpace(queryNorm)
	if query == "" {
		return 0.0
	}

	title := NormalizeForMatch(result.Title)
	candidates := []float64{TokenSortRatio(query, title)}

	if result.Subtitle != "" {
		combined := strings.TrimSpace(NormalizeForMatch(result.Subtitle) + " " + title)
		candidates = append(candidates, TokenSortRatio(query, combined))
	}

	queryCW := contentWords(query)
	if len(queryCW) > 0 {
		queryC := sortedJoin(queryCW)
		titleC := sortedJoin(contentWords(title))
		candidates = append(candidates, TokenSortRatio(queryC, titleC))
		if result.Subtitle != "" {
			combined := strings.TrimSpace(NormalizeForMatch(result.Subtitle) + " " + title)
			combinedC := sortedJoin(contentWords(combined))
			candidates = append(candidates, TokenSortRatio(queryC, combinedC))
		}
	}

	best := 0.0
	for _, c := range candidates {
		if c > best {
			best = c
		}
	}
	return best / 100.0
}

func sortedJoin(words map[string]bool) string {
	sorted := make([]string, 0, len(words))
	for w := range words {
		sorted = append(sorted, w)
	}
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

type ranked struct {
	result   domain.SearchResult
	bestRank map[domain.ProviderName]int
}

type scored struct {
	result    domain.SearchResult
	relevance float64
	rrf       float64
}

// FuseAndRank merges on identifiers, ranks by relevance.
// perProvider is a slice of per-provider result groups (native order preserved).
func FuseAndRank(perProvider [][]domain.SearchResult, queryNorm string, qualityScorer func(domain.SearchResult) domain.QualityScore) []domain.SearchResult {
	// Pre-merge album-name stabilization
	albumBest := make(map[string]struct {
		album string
		comp  int
	})
	for _, group := range perProvider {
		for _, r := range group {
			album := getStringExtra(r, "album")
			if album == "" {
				continue
			}
			sig := signature(r)
			comp := completeness(r)
			prev, ok := albumBest[sig]
			if !ok || comp > prev.comp {
				albumBest[sig] = struct {
					album string
					comp  int
				}{album, comp}
			}
		}
	}

	var accumulated []ranked
	for _, group := range perProvider {
		for rank, incoming := range group {
			candidate := asLowConfidence(incoming)
			candProviders := providersOf(candidate)

			merged := false
			for i := range accumulated {
				result, ok := tryMerge(accumulated[i].result, candidate)
				if ok {
					accumulated[i].result = result
					for provider := range candProviders {
						prev, exists := accumulated[i].bestRank[provider]
						if !exists || rank < prev {
							accumulated[i].bestRank[provider] = rank
						}
					}
					merged = true
					break
				}
			}
			if !merged {
				ranks := make(map[domain.ProviderName]int)
				for p := range candProviders {
					ranks[p] = rank
				}
				accumulated = append(accumulated, ranked{result: candidate, bestRank: ranks})
			}
		}
	}

	// Post-merge album-name stabilization
	for i := range accumulated {
		sig := signature(accumulated[i].result)
		best, ok := albumBest[sig]
		if ok && getStringExtra(accumulated[i].result, "album") != best.album {
			extras := copyExtras(accumulated[i].result.Extras)
			extras["album"] = best.album
			accumulated[i].result.Extras = extras
		}
	}

	// Normalize raw provider popularity into a 0-100 score
	for i := range accumulated {
		pop := NormalizePopularity(accumulated[i].result.Extras)
		if pop > 0 {
			extras := copyExtras(accumulated[i].result.Extras)
			extras["popularity"] = pop
			accumulated[i].result.Extras = extras
		}
	}

	// Score and gate
	var scoredResults []scored
	for _, entry := range accumulated {
		rrf := 0.0
		for _, rank := range entry.bestRank {
			rrf += 1.0 / (float64(rrfK) + float64(rank))
		}

		r := entry.result
		if len(providersOf(r)) < 2 && r.Confidence != domain.ConfidenceLow {
			r = asLowConfidence(r)
		}

		extras := copyExtras(r.Extras)
		extras["_rrf"] = rrf
		r.Extras = extras

		rel := relevanceScore(r, queryNorm)
		if strings.TrimSpace(queryNorm) != "" && !sharesWord(r, queryNorm) {
			continue
		}
		if !hasBrowseableSource(r) {
			continue
		}
		scoredResults = append(scoredResults, scored{result: r, relevance: rel, rrf: rrf})
	}

	sort.SliceStable(scoredResults, func(i, j int) bool {
		return rankingKeyLess(scoredResults[i], scoredResults[j], qualityScorer)
	})

	results := make([]domain.SearchResult, len(scoredResults))
	for i, s := range scoredResults {
		results[i] = s.result
	}
	return results
}

// Rerank re-sorts after enrichment. Same key minus quality_score.
func Rerank(results []domain.SearchResult, queryNorm string) []domain.SearchResult {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		bandA := roundTo1(relevanceScore(a, queryNorm))
		bandB := roundTo1(relevanceScore(b, queryNorm))
		if bandA != bandB {
			return bandA > bandB
		}
		demA := boolToInt(IsDemoted(a))
		demB := boolToInt(IsDemoted(b))
		if demA != demB {
			return demA < demB
		}
		multiA := boolToInt(len(providersOf(a)) > 1)
		multiB := boolToInt(len(providersOf(b)) > 1)
		if multiA != multiB {
			return multiA > multiB
		}
		popA := popularity(a)
		popB := popularity(b)
		if popA != popB {
			return popA > popB
		}
		rrfA := getFloatExtra(a, "_rrf")
		rrfB := getFloatExtra(b, "_rrf")
		if rrfA != rrfB {
			return rrfA > rrfB
		}
		if a.Subtitle != b.Subtitle {
			return a.Subtitle < b.Subtitle
		}
		return a.Title < b.Title
	})
	return results
}

// rankingKeyLess implements the multi-criteria sort:
// (-band, demoted, -multi_source, -popularity, -q_score, -rrf, subtitle, title)
func rankingKeyLess(a, b scored, qualityScorer func(domain.SearchResult) domain.QualityScore) bool {
	bandA := roundTo1(a.relevance)
	bandB := roundTo1(b.relevance)
	if bandA != bandB {
		return bandA > bandB
	}
	demA := boolToInt(IsDemoted(a.result))
	demB := boolToInt(IsDemoted(b.result))
	if demA != demB {
		return demA < demB
	}
	multiA := boolToInt(len(providersOf(a.result)) > 1)
	multiB := boolToInt(len(providersOf(b.result)) > 1)
	if multiA != multiB {
		return multiA > multiB
	}
	popA := popularity(a.result)
	popB := popularity(b.result)
	if popA != popB {
		return popA > popB
	}
	var qA, qB float64
	if qualityScorer != nil {
		qA = qualityScorer(a.result).Completeness
		qB = qualityScorer(b.result).Completeness
	}
	if qA != qB {
		return qA > qB
	}
	if a.rrf != b.rrf {
		return a.rrf > b.rrf
	}
	if a.result.Subtitle != b.result.Subtitle {
		return a.result.Subtitle < b.result.Subtitle
	}
	return a.result.Title < b.result.Title
}

func roundTo1(f float64) float64 {
	return math.Round(f*10) / 10
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func copyExtras(src map[string]any) map[string]any {
	if src == nil {
		return make(map[string]any)
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func getStringExtra(r domain.SearchResult, key string) string {
	if r.Extras == nil {
		return ""
	}
	v, ok := r.Extras[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// hasBrowseableSource returns true if the result has at least one source
// from a provider that supports catalog-browse (detail screen content).
// Tracks always pass (they don't need catalog-browse — detail is the
// handoff data). Artists and albums need a Deezer source to load
// top-tracks / albums / tracklist.
func hasBrowseableSource(r domain.SearchResult) bool {
	if r.Kind == domain.ResultKindTrack {
		return true
	}
	for _, s := range r.Sources {
		if s.Provider == domain.ProviderDeezer {
			return true
		}
	}
	return false
}

func getFloatExtra(r domain.SearchResult, key string) float64 {
	if r.Extras == nil {
		return 0.0
	}
	v, ok := r.Extras[key]
	if !ok {
		return 0.0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0.0
	}
}
