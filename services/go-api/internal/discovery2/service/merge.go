package service

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
	legacy "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

// Layer 2 — merge + entity resolution.
//
// "Same entity?" is decided by a categorical cascade that consults exact/cheap
// signals first and falls back to a single fuzzy rung only as a documented last
// resort:
//
//  1. Identifier match — equal ISRC or equal MBID is the same entity; distinct
//     MBIDs are authoritatively distinct. Exact, no threshold.
//  2. Version-marker categories — a title is parsed into (core, version tags),
//     where tags are categorical (sequel number, remix, live, feat. X, deluxe,
//     …). Same core + same tags = same work; same core + different tags =
//     different work (kept separate). This dissolves Pattern B — a numbered
//     sequel can never collapse into the original, because no similarity number
//     is consulted.
//  3. Fuzzy last resort — the ONLY surviving threshold, used for the typo
//     residual after 1–2 decide nothing. It never overrides a version-marker
//     difference, so it cannot resurrect Pattern B.

// fuzzyCoreThreshold is the sole surviving similarity threshold in Layer 2 (the
// doctrine's documented "last resort"). After the identifier and version-marker
// rungs decide nothing, two same-artist titles whose cores match at or above
// this TokenSortRatio are treated as the same work — catching typos and
// spelling/punctuation variance ("Bohemian Rhapsody" vs "Bohemian Rapsody").
// Eval-gated; never applied across a version-marker difference.
const fuzzyCoreThreshold = 85.0

// Entity is a merged search result plus per-provider rank provenance: the best
// (lowest) position at which this entity surfaced in each provider's native
// ordering. Layer 3 consumes BestRank for the RRF within-tier tiebreak.
type Entity struct {
	Result   domain.SearchResult
	BestRank map[domain.ProviderName]int
}

// Merge collapses per-provider result groups into deduped entities via the
// categorical cascade. Sources are unioned; the most complete variant becomes
// canonical. Native per-provider ordering is preserved as BestRank.
func Merge(perProvider [][]domain.SearchResult) []Entity {
	entities := make([]Entity, 0)
	for _, group := range perProvider {
		for rank, c := range group {
			cProviders := providersOf(c)

			merged := false
			for i := range entities {
				tier, ok := sameEntity(entities[i].Result, c)
				if !ok {
					continue
				}
				entities[i].Result = mergeInto(entities[i].Result, c, tier)
				for p := range cProviders {
					if prev, exists := entities[i].BestRank[p]; !exists || rank < prev {
						entities[i].BestRank[p] = rank
					}
				}
				merged = true
				break
			}
			if merged {
				continue
			}

			ranks := make(map[domain.ProviderName]int, len(cProviders))
			for p := range cProviders {
				ranks[p] = rank
			}
			entities = append(entities, Entity{Result: c, BestRank: ranks})
		}
	}
	return entities
}

// sameEntity runs the categorical cascade and, when two results are the same
// entity, reports the strongest resolution tier that proved it.
func sameEntity(e, c domain.SearchResult) (domain.EntityResolutionTier, bool) {
	if e.Kind != c.Kind {
		return domain.EntityResolutionNone, false
	}

	// Rung 1 — identifier match (exact).
	if ie, ic := stringExtra(e, "isrc"), stringExtra(c, "isrc"); ie != "" && ic != "" && ie == ic {
		return domain.EntityResolutionISRC, true
	}
	if me, mc := stringExtra(e, "mbid"), stringExtra(c, "mbid"); me != "" && mc != "" {
		if me == mc {
			return domain.EntityResolutionMBID, true
		}
		return domain.EntityResolutionNone, false
	}

	// Artists resolve by name alone — no version markers, no artist subtitle.
	if e.Kind == domain.ResultKindArtist {
		same := textnorm.NormalizeForMatch(e.Title) == textnorm.NormalizeForMatch(c.Title)
		return domain.EntityResolutionNone, same
	}

	// Tracks/albums must share the same artist before any title comparison.
	if textnorm.NormalizeForMatch(e.Subtitle) != textnorm.NormalizeForMatch(c.Subtitle) {
		return domain.EntityResolutionNone, false
	}

	ke, kc := parseVersion(e.Title), parseVersion(c.Title)

	// Rung 2 — version-marker categories.
	if ke.tags != kc.tags {
		return domain.EntityResolutionNone, false
	}
	if ke.core == kc.core {
		return domain.EntityResolutionNone, true
	}

	// Rung 3 — fuzzy last resort.
	if legacy.TokenSortRatio(ke.core, kc.core) >= fuzzyCoreThreshold {
		return domain.EntityResolutionNone, true
	}
	return domain.EntityResolutionNone, false
}

// mergeInto folds other into canonical: the more complete result wins title/
// subtitle/image, sources are unioned, popularity is the max, and the merge's
// resolution tier and display confidence are recorded.
func mergeInto(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) domain.SearchResult {
	if completenessOf(other) > completenessOf(canonical) {
		canonical, other = other, canonical
	}

	seen := make(map[string]bool, len(canonical.Sources)+len(other.Sources))
	sources := make([]domain.SourceRef, 0, len(canonical.Sources)+len(other.Sources))
	for _, s := range append(append([]domain.SourceRef{}, canonical.Sources...), other.Sources...) {
		key := s.Provider.String() + ":" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, s)
	}

	extras := make(map[string]any, len(canonical.Extras)+len(other.Extras))
	for k, v := range other.Extras {
		extras[k] = v
	}
	for k, v := range canonical.Extras {
		if v != nil || extras[k] == nil {
			extras[k] = v
		}
	}
	if pop := math.Max(popularityOf(canonical), popularityOf(other)); pop > 0 {
		extras["popularity"] = pop
	}
	extras["resolution_tier"] = tier.String()

	imageURL := canonical.ImageURL
	if imageURL == "" {
		imageURL = other.ImageURL
	}

	conf := domain.ConfidenceLow
	switch tier {
	case domain.EntityResolutionISRC, domain.EntityResolutionMBID:
		conf = domain.ConfidenceHigh
	default:
		if len(sources) > 1 {
			conf = domain.ConfidenceMedium
		}
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

// versionKey is a title decomposed into its core work and the sorted set of
// categorical markers that distinguish one release of that work from another.
type versionKey struct {
	core string
	tags string
}

var (
	// parenFeatRe matches a feat clause that fills a parenthetical segment.
	parenFeatRe = regexp.MustCompile(`(?i)^\s*(?:feat\.?|ft\.?|featuring|f/)\s+(.+)$`)
	// trailingFeatRe matches a trailing, non-parenthesized feat clause.
	trailingFeatRe = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|f/)\s+(.+?)\s*$`)
	// parenSegmentRe matches a parenthetical or bracketed segment.
	parenSegmentRe = regexp.MustCompile(`[\(\[]([^)\]]*)[\)\]]`)
	// partRe matches a trailing "Part N" / "Pt. N" sequel marker (N arabic or roman).
	partRe = regexp.MustCompile(`(?i)\b(?:pt|part)\.?\s*(\d{1,3}|[ivx]+)\s*$`)
	// trailingNumRe matches a trailing bare integer sequel marker.
	trailingNumRe = regexp.MustCompile(`\s+(\d{1,3})\s*$`)
)

// parseVersion decomposes a title into its core work and categorical version
// markers. Recognized markers (feat, qualifier keywords, sequel numbers) are
// lifted into tags; unrecognized parentheticals stay in the core — e.g.
// "(I Can't Get No) Satisfaction" keeps its parenthetical as part of the title.
func parseVersion(title string) versionKey {
	s := strings.ToLower(strings.TrimSpace(title))
	var tags []string

	// Parenthetical / bracketed segments: a feat clause (featured artist
	// distinguishes the work) or a qualifier keyword (remix, live, deluxe, …).
	s = parenSegmentRe.ReplaceAllStringFunc(s, func(seg string) string {
		inner := strings.Trim(seg, "()[]")
		if m := parenFeatRe.FindStringSubmatch(inner); m != nil {
			tags = append(tags, "feat:"+textnorm.NormalizeForMatch(m[1]))
			return " "
		}
		if cat, ok := classifyQualifier(inner); ok {
			tags = append(tags, cat)
			return " "
		}
		return seg
	})

	// Trailing non-parenthesized feat clause: "Sicko Mode feat. Drake".
	if m := trailingFeatRe.FindStringSubmatch(s); m != nil {
		tags = append(tags, "feat:"+textnorm.NormalizeForMatch(m[1]))
		s = strings.TrimSpace(trailingFeatRe.ReplaceAllString(s, ""))
	}

	// Trailing dash-suffix qualifier: "title - remastered 2011", "title - live".
	if idx := strings.LastIndex(s, " - "); idx >= 0 {
		if cat, ok := classifyQualifier(s[idx+3:]); ok {
			tags = append(tags, cat)
			s = s[:idx]
		}
	}
	s = strings.TrimSpace(s)

	// Sequel markers: "Part N" / "Pt. N", else a trailing bare integer >= 2.
	if m := partRe.FindStringSubmatch(s); m != nil {
		if n := romanOrInt(m[1]); n >= 1 {
			tags = append(tags, "n:"+strconv.Itoa(n))
			s = strings.TrimSpace(partRe.ReplaceAllString(s, ""))
		}
	} else if m := trailingNumRe.FindStringSubmatch(s); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 2 {
			tags = append(tags, "n:"+strconv.Itoa(n))
			s = strings.TrimSpace(trailingNumRe.ReplaceAllString(s, ""))
		}
	}

	sort.Strings(tags)
	return versionKey{
		core: textnorm.NormalizeForMatch(s),
		tags: strings.Join(tags, "|"),
	}
}

// qualifierCategories maps marker keywords to a canonical category. Order is
// priority order: the first category whose keyword appears in a segment wins,
// so each segment yields at most one tag.
//
// AIDEV-NOTE: This is the categorical version-marker vocabulary (plan 003
// Layer 2). It is a structural definition, not a tuned constant — extend it
// when the eval surfaces an unhandled marker format; unmatched segments fall
// through to the core (and, if needed, the fuzzy rung), never silently merged.
var qualifierCategories = []struct {
	cat      string
	keywords []string
}{
	{"remix", []string{"remix", "rmx"}},
	{"live", []string{"live"}},
	{"acoustic", []string{"acoustic"}},
	{"instrumental", []string{"instrumental"}},
	{"remaster", []string{"remaster"}},
	{"deluxe", []string{"deluxe"}},
	{"extended", []string{"extended"}},
	{"sped", []string{"sped up", "sped-up", "speed up"}},
	{"slowed", []string{"slowed", "reverb"}},
	{"radio", []string{"radio"}},
	{"edit", []string{"edit"}},
	{"version", []string{"version"}},
	{"mix", []string{"mix"}},
	{"demo", []string{"demo"}},
	{"cover", []string{"cover"}},
	{"edition", []string{"edition"}},
	{"mono", []string{"mono"}},
	{"stereo", []string{"stereo"}},
	{"bonus", []string{"bonus"}},
	{"reprise", []string{"reprise"}},
	{"unplugged", []string{"unplugged"}},
	{"session", []string{"session"}},
	{"vip", []string{"vip"}},
	{"bootleg", []string{"bootleg"}},
	{"rework", []string{"rework"}},
	{"original", []string{"original"}},
}

func classifyQualifier(seg string) (string, bool) {
	seg = strings.ToLower(strings.Trim(strings.TrimSpace(seg), "()[]"))
	if seg == "" {
		return "", false
	}
	for _, q := range qualifierCategories {
		for _, kw := range q.keywords {
			if strings.Contains(seg, kw) {
				return q.cat, true
			}
		}
	}
	return "", false
}

func romanOrInt(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ii":
		return 2
	case "iii":
		return 3
	case "iv":
		return 4
	case "v":
		return 5
	case "vi":
		return 6
	case "vii":
		return 7
	case "viii":
		return 8
	case "ix":
		return 9
	case "x":
		return 10
	}
	return 0
}

func stringExtra(r domain.SearchResult, key string) string {
	if r.Extras == nil {
		return ""
	}
	if v, ok := r.Extras[key].(string); ok {
		return v
	}
	return ""
}

func popularityOf(r domain.SearchResult) float64 {
	if r.Extras == nil {
		return 0
	}
	switch n := r.Extras["popularity"].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

func completenessOf(r domain.SearchResult) int {
	n := 0
	if r.ImageURL != "" {
		n++
	}
	if stringExtra(r, "isrc") != "" {
		n++
	}
	if r.Extras != nil {
		if _, ok := r.Extras["duration"]; ok {
			n++
		}
	}
	if stringExtra(r, "album") != "" {
		n++
	}
	return n
}

func providersOf(r domain.SearchResult) map[domain.ProviderName]bool {
	m := make(map[domain.ProviderName]bool, len(r.Sources))
	for _, s := range r.Sources {
		m[s.Provider] = true
	}
	return m
}
