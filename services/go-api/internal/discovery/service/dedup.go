package service

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// versionSimilarityThreshold is the minimum TokenSortRatio (0-100) for two
// results to be considered versions of the same work (e.g., remix vs original).
// 85 was chosen empirically: high enough to avoid false positives on short
// titles ("Love" vs "Lover"), low enough to catch "(Deluxe)" and "(Remix)" variants.
const versionSimilarityThreshold = 85

var collabMarkers = []string{"feat.", "feat ", "ft.", "ft ", "featuring "}

const rrfK = 60

var providerRRFWeight = map[domain.ProviderName]float64{
	domain.ProviderDeezer:      1.2,
	domain.ProviderMusicBrainz: 1.1,
	domain.ProviderLastFM:      1.0,
	domain.ProviderITunes:      0.9,
	domain.ProviderSoundCloud:  0.8,
	domain.ProviderTheAudioDB:  0.7,
}

const (
	diversityWindow    = 10
	maxPerArtistInTop  = 3
)

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
		if _, ok := r.Extras["duration"]; ok {
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
	score := best / 100.0

	// Enhancement A: exact title match bonus
	if query == title {
		score = math.Min(1.0, score+0.05)
	}

	// Enhancement D: multi-field bonus — all query words present in title+subtitle
	if result.Subtitle != "" && len(queryCW) > 1 {
		combined := strings.TrimSpace(NormalizeForMatch(result.Subtitle) + " " + title)
		combinedWords := contentWords(combined)
		if allWordsPresent(queryCW, combinedWords) {
			score = math.Min(1.0, score+0.03)
		}
	}

	// Enhancement E: prefix match bonus
	if len(query) >= 3 && strings.HasPrefix(title, query) {
		score = math.Min(1.0, score+0.03)
	}

	return score
}

func allWordsPresent(query, text map[string]bool) bool {
	for w := range query {
		if !text[w] {
			return false
		}
	}
	return true
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
func FuseAndRank(perProvider [][]domain.SearchResult, queryNorm string, qualityScorer func(domain.SearchResult) domain.QualityScore, intent *QueryIntent) []domain.SearchResult {
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

	transferArtistDisambiguation(accumulated)

	// Normalize raw provider popularity into a 0-100 score.
	// Albums and artists from search often lack explicit popularity metrics
	// (Deezer album search returns nb_fan=0). For those, derive a score from
	// their Deezer result position within the same kind — Deezer already ranks
	// by relevance/popularity. The kind-local position is used because providers
	// return all kinds combined (tracks at 0..14, albums at 15..29, etc.).
	kindDeezerCount := make(map[domain.ResultKind]int)
	for i := range accumulated {
		pop := NormalizePopularity(accumulated[i].result.Extras)
		if pop > 0 {
			extras := copyExtras(accumulated[i].result.Extras)
			extras["popularity"] = pop
			accumulated[i].result.Extras = extras
		} else if _, ok := accumulated[i].bestRank[domain.ProviderDeezer]; ok {
			kind := accumulated[i].result.Kind
			kindPos := kindDeezerCount[kind]
			kindDeezerCount[kind]++
			pop = positionalPopularity(kindPos)
			if pop > 0 {
				extras := copyExtras(accumulated[i].result.Extras)
				extras["popularity"] = pop
				accumulated[i].result.Extras = extras
			}
		}
	}

	// Recency boost before scoring so boosted popularity feeds into the sort
	accResults := make([]domain.SearchResult, len(accumulated))
	for i := range accumulated {
		accResults[i] = accumulated[i].result
	}
	accResults = applyRecencyBoost(accResults)
	for i := range accumulated {
		accumulated[i].result = accResults[i]
	}

	// Prepare intent matching if detected
	var intentArtistNorm, intentTrackNorm string
	if intent != nil {
		intentArtistNorm = NormalizeForMatch(intent.Artist)
		intentTrackNorm = NormalizeForMatch(intent.Track)
	}

	// Score and gate
	var scoredResults []scored
	for _, entry := range accumulated {
		rrf := 0.0
		for provider, rank := range entry.bestRank {
			w := providerRRFWeight[provider]
			if w == 0 {
				w = 1.0
			}
			rrf += w / (float64(rrfK) + float64(rank))
		}

		r := entry.result
		if len(providersOf(r)) < 2 && r.Confidence != domain.ConfidenceLow {
			r = asLowConfidence(r)
		}

		extras := copyExtras(r.Extras)
		extras["_rrf"] = rrf
		r.Extras = extras

		rel := relevanceScore(r, queryNorm)

		// Intent boost: add to relevance when both artist and track match
		if intent != nil {
			subtitleNorm := NormalizeForMatch(r.Subtitle)
			titleNorm := NormalizeForMatch(r.Title)
			if strings.Contains(subtitleNorm, intentArtistNorm) && strings.Contains(titleNorm, intentTrackNorm) {
				rel = math.Min(1.0, rel+intentBoost)
			}
		}

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

	logLimit := 10
	if len(scoredResults) < logLimit {
		logLimit = len(scoredResults)
	}
	for i := 0; i < logLimit; i++ {
		s := scoredResults[i]
		slog.Debug("search.ranking",
			"pos", i+1,
			"kind", s.result.Kind.String(),
			"title", s.result.Title,
			"subtitle", s.result.Subtitle,
			"relevance", roundBand(s.relevance),
			"popularity", popularity(s.result),
			"sources", len(s.result.Sources),
			"rrf", s.rrf,
		)
	}

	results := make([]domain.SearchResult, len(scoredResults))
	for i, s := range scoredResults {
		results[i] = s.result
	}

	collapsed := CollapseVersions(results)
	reordered := ApplyPopularityDominance(collapsed)
	return EnforceDiversity(reordered)
}

// Rerank re-sorts after enrichment. Same key minus quality_score —
// quality_score is excluded deliberately because enrichment changes
// completeness (artwork, popularity), so including it would cause
// unstable reordering between pre- and post-enrichment sorts.
func Rerank(results []domain.SearchResult, queryNorm string) []domain.SearchResult {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		bandA := roundBand(relevanceScore(a, queryNorm))
		bandB := roundBand(relevanceScore(b, queryNorm))
		if bandA != bandB {
			return bandA > bandB
		}
		demA := boolToInt(IsDemoted(a))
		demB := boolToInt(IsDemoted(b))
		if demA != demB {
			return demA < demB
		}
		popA := bandPop(effectivePop(a))
		popB := bandPop(effectivePop(b))
		if popA != popB {
			return popA > popB
		}
		multiA := boolToInt(len(providersOf(a)) > 1)
		multiB := boolToInt(len(providersOf(b)) > 1)
		if multiA != multiB {
			return multiA > multiB
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

	logLimit := 10
	if len(results) < logLimit {
		logLimit = len(results)
	}
	for i := 0; i < logLimit; i++ {
		r := results[i]
		slog.Debug("search.rerank",
			"pos", i+1,
			"kind", r.Kind.String(),
			"title", r.Title,
			"subtitle", r.Subtitle,
			"relevance", roundBand(relevanceScore(r, queryNorm)),
			"popularity", popularity(r),
			"sources", len(r.Sources),
		)
	}

	return results
}

// rankingKeyLess implements the multi-criteria sort:
// (-band, demoted, -popularity, -multi_source, -q_score, -rrf, subtitle, title)
func rankingKeyLess(a, b scored, qualityScorer func(domain.SearchResult) domain.QualityScore) bool {
	bandA := roundBand(a.relevance)
	bandB := roundBand(b.relevance)
	if bandA != bandB {
		return bandA > bandB
	}
	demA := boolToInt(IsDemoted(a.result))
	demB := boolToInt(IsDemoted(b.result))
	if demA != demB {
		return demA < demB
	}
	popA := bandPop(effectivePop(a.result))
	popB := bandPop(effectivePop(b.result))
	if popA != popB {
		return popA > popB
	}
	multiA := boolToInt(len(providersOf(a.result)) > 1)
	multiB := boolToInt(len(providersOf(b.result)) > 1)
	if multiA != multiB {
		return multiA > multiB
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

func roundBand(f float64) float64 {
	return math.Round(f*20) / 20
}

// bandPop quantizes popularity to reduce noise: 5-point bands below 90,
// 3-point bands at 90+ so mega-hits remain distinguishable.
func bandPop(p float64) float64 {
	if p >= 90 {
		return math.Floor(p/3) * 3
	}
	return math.Floor(p/5) * 5
}

// artistSourceBonus is the popularity bonus per additional provider for artist
// results. Artists naturally appear across all providers by name — a 6-source
// artist is almost certainly the canonical entity. Tracks get multi-source
// through ISRC/MBID merge which is a different (already handled) signal.
const artistSourceBonus = 5

func effectivePop(r domain.SearchResult) float64 {
	pop := popularity(r)
	if r.Kind != domain.ResultKindArtist {
		return pop
	}
	extra := len(r.Sources) - 1
	if extra <= 0 {
		return pop
	}
	return math.Min(100, pop+float64(extra)*artistSourceBonus)
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

type versionCluster struct {
	bestIdx   int
	bestPop   float64
	titleNorm string
	hasCollab bool
	count     int
}

func hasCollaboration(title string) bool {
	lower := strings.ToLower(title)
	for _, m := range collabMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// CollapseVersions groups results that are versions of the same recording
// using title similarity on normalized titles. A collaboration guard
// prevents collapsing "Song" with "Song (feat. Artist)" since those are
// different recordings. Distinct MBIDs are never collapsed.
func CollapseVersions(results []domain.SearchResult) []domain.SearchResult {
	clusterOf := make([]int, len(results))
	clusters := make([]*versionCluster, 0)

	for i, r := range results {
		if r.Kind == domain.ResultKindArtist {
			clusterOf[i] = len(clusters)
			clusters = append(clusters, &versionCluster{
				bestIdx: i, bestPop: popularity(r),
				titleNorm: NormalizeForMatch(r.Title), count: 1,
			})
			continue
		}
		titleNorm := NormalizeForMatch(r.Title)
		artistNorm := NormalizeForMatch(r.Subtitle)
		kind := r.Kind.String()
		mbid := getStringExtra(r, "mbid")
		pop := popularity(r)
		collab := hasCollaboration(r.Title)

		matched := -1
		for ci, c := range clusters {
			cr := results[c.bestIdx]
			if cr.Kind.String() != kind {
				continue
			}
			if NormalizeForMatch(cr.Subtitle) != artistNorm {
				continue
			}
			cMbid := getStringExtra(cr, "mbid")
			if cr.Kind == domain.ResultKindArtist && (mbid != "" || cMbid != "") {
				continue
			}
			if mbid != "" && cMbid != "" && mbid != cMbid {
				continue
			}
			if collab != c.hasCollab {
				continue
			}
			if TokenSortRatio(titleNorm, c.titleNorm) >= versionSimilarityThreshold {
				matched = ci
				break
			}
		}

		if matched >= 0 {
			c := clusters[matched]
			c.count++
			if pop > c.bestPop {
				c.bestIdx = i
				c.bestPop = pop
				c.titleNorm = titleNorm
			}
			clusterOf[i] = matched
		} else {
			clusterOf[i] = len(clusters)
			clusters = append(clusters, &versionCluster{
				bestIdx:   i,
				bestPop:   pop,
				titleNorm: titleNorm,
				hasCollab: collab,
				count:     1,
			})
		}
	}

	emitted := make(map[int]bool)
	out := make([]domain.SearchResult, 0, len(clusters))
	for i := range results {
		ci := clusterOf[i]
		if emitted[ci] {
			continue
		}
		emitted[ci] = true
		c := clusters[ci]
		r := results[c.bestIdx]
		if c.count > 1 {
			r = withVariantCount(r, c.count)
		}
		out = append(out, r)
	}
	return out
}

func withVariantCount(r domain.SearchResult, count int) domain.SearchResult {
	extras := copyExtras(r.Extras)
	extras["variant_count"] = count
	r.Extras = extras
	return r
}

const recencyWindowDays = 30
const recencyMultiplier = 1.1

func applyRecencyBoost(results []domain.SearchResult) []domain.SearchResult {
	now := time.Now()
	for i, r := range results {
		results[i] = boostIfRecent(r, now)
	}
	return results
}

func boostIfRecent(r domain.SearchResult, now time.Time) domain.SearchResult {
	rd := parseReleaseDate(getStringExtra(r, "release_date"))
	if rd.IsZero() {
		return r
	}
	cutoff := now.AddDate(0, 0, -recencyWindowDays)
	if rd.Before(cutoff) {
		return r
	}
	pop := popularity(r)
	boosted := math.Min(100, pop*recencyMultiplier)
	extras := copyExtras(r.Extras)
	extras["popularity"] = boosted
	r.Extras = extras
	return r
}

func parseReleaseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006", s); err == nil {
		return t
	}
	return time.Time{}
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

const (
	popularityDominanceWindow    = 5
	popularityDominanceGapAbs    = 20
	popularityDominanceGapFactor = 3.0
)

// ApplyPopularityDominance ensures that when top results span different kinds
// (e.g., artist "Humble" vs track "Humble" by Kendrick Lamar), the most
// popular result wins. Results are already sorted by the ranking key, so
// items in the top window have similar relevance. This only fires when the
// popularity gap is decisive.
func ApplyPopularityDominance(results []domain.SearchResult) []domain.SearchResult {
	if len(results) < 2 {
		return results
	}

	topPop := popularity(results[0])
	topKind := results[0].Kind

	limit := popularityDominanceWindow
	if len(results) < limit {
		limit = len(results)
	}

	bestIdx := 0
	bestPop := topPop
	for i := 1; i < limit; i++ {
		if results[i].Kind == topKind {
			continue
		}
		pop := popularity(results[i])
		if pop > bestPop {
			bestIdx = i
			bestPop = pop
		}
	}

	if bestIdx == 0 {
		return results
	}
	gapSufficient := bestPop-topPop >= popularityDominanceGapAbs
	ratioSufficient := bestPop >= topPop*popularityDominanceGapFactor
	if !gapSufficient && !ratioSufficient {
		return results
	}

	slog.Debug("search.popularity_dominance",
		"promoted_kind", results[bestIdx].Kind.String(),
		"promoted_title", results[bestIdx].Title,
		"promoted_pop", bestPop,
		"displaced_kind", results[0].Kind.String(),
		"displaced_title", results[0].Title,
		"displaced_pop", topPop,
	)

	out := make([]domain.SearchResult, 0, len(results))
	out = append(out, results[bestIdx])
	for i, r := range results {
		if i != bestIdx {
			out = append(out, r)
		}
	}
	return out
}

// EnforceDiversity limits the number of results per artist within the
// top diversityWindow positions to maxPerArtistInTop, moving overflow
// results below the window.
func EnforceDiversity(results []domain.SearchResult) []domain.SearchResult {
	if len(results) < diversityWindow {
		return results
	}
	window := results[:diversityWindow]
	rest := results[diversityWindow:]

	artistCount := make(map[string]int)
	kept := make([]domain.SearchResult, 0, diversityWindow)
	overflow := make([]domain.SearchResult, 0)

	for _, r := range window {
		artist := NormalizeForMatch(r.Subtitle)
		if artist == "" || artistCount[artist] < maxPerArtistInTop {
			artistCount[artist]++
			kept = append(kept, r)
		} else {
			overflow = append(overflow, r)
		}
	}

	out := make([]domain.SearchResult, 0, len(results))
	out = append(out, kept...)
	out = append(out, overflow...)
	out = append(out, rest...)
	return out
}

// transferArtistDisambiguation copies disambiguation metadata from MB-sourced
// artists to matching non-MB artists with the same normalized name. MB artists
// are later filtered by hasBrowseableSource (no Deezer source), but their
// disambiguation text should survive on the Deezer results that pass the gate.
func transferArtistDisambiguation(accumulated []ranked) {
	type disambigEntry struct {
		disambig string
		mbid     string
	}
	byName := make(map[string]disambigEntry)
	for _, entry := range accumulated {
		r := entry.result
		if r.Kind != domain.ResultKindArtist {
			continue
		}
		disambig := getStringExtra(r, "disambiguation")
		mbid := getStringExtra(r, "mbid")
		if disambig == "" || mbid == "" {
			continue
		}
		norm := NormalizeForMatch(r.Title)
		if _, exists := byName[norm]; !exists {
			byName[norm] = disambigEntry{disambig: disambig, mbid: mbid}
		}
	}

	for i := range accumulated {
		r := &accumulated[i].result
		if r.Kind != domain.ResultKindArtist {
			continue
		}
		if getStringExtra(*r, "disambiguation") != "" {
			continue
		}
		norm := NormalizeForMatch(r.Title)
		if entry, ok := byName[norm]; ok {
			extras := copyExtras(r.Extras)
			extras["disambiguation"] = entry.disambig
			if getStringExtra(*r, "mbid") == "" {
				extras["mbid"] = entry.mbid
			}
			r.Extras = extras
		}
	}
}

// CollapseArtistDuplicates groups artist results that share the same normalized
// name. The highest-popularity artist is kept as the primary result. Remaining
// same-name artists are stored in a "collapsed_artists" extra on the primary.
func CollapseArtistDuplicates(results []domain.SearchResult) []domain.SearchResult {
	type group struct {
		primaryIdx int
		primaryPop float64
		otherIdxs  []int
	}
	groups := make(map[string]*group)
	order := []string{}

	for i, r := range results {
		if r.Kind != domain.ResultKindArtist {
			continue
		}
		norm := NormalizeForMatch(r.Title)
		pop := getFloatExtra(r, "popularity")
		g, exists := groups[norm]
		if !exists {
			groups[norm] = &group{primaryIdx: i, primaryPop: pop}
			order = append(order, norm)
			continue
		}
		if pop > g.primaryPop {
			g.otherIdxs = append(g.otherIdxs, g.primaryIdx)
			g.primaryIdx = i
			g.primaryPop = pop
		} else {
			g.otherIdxs = append(g.otherIdxs, i)
		}
	}

	remove := make(map[int]bool)
	for _, norm := range order {
		g := groups[norm]
		if len(g.otherIdxs) == 0 {
			continue
		}
		collapsed_list := make([]map[string]any, len(g.otherIdxs))
		for j, idx := range g.otherIdxs {
			other := results[idx]
			collapsed_list[j] = map[string]any{
				"title":    other.Title,
				"subtitle": other.Subtitle,
				"sources":  other.Sources,
				"extras":   other.Extras,
			}
			if other.ImageURL != "" {
				collapsed_list[j]["image_url"] = other.ImageURL
			}
			remove[idx] = true
		}
		primary := &results[g.primaryIdx]
		extras := copyExtras(primary.Extras)
		extras["collapsed_artists"] = collapsed_list
		primary.Extras = extras
	}

	if len(remove) == 0 {
		return results
	}

	out := make([]domain.SearchResult, 0, len(results)-len(remove))
	for i, r := range results {
		if !remove[i] {
			out = append(out, r)
		}
	}
	return out
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
