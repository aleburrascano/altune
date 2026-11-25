package rank

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/albumgrouping"
	"altune/go-api/internal/shared/songfields"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/titlemarkers"
	"math"
	"slices"
)

type Entity struct {
	Result   domain.SearchResult
	BestRank map[domain.ProviderName]int
}

const maxMergeResultsPerProvider = 100

func capPerProvider(perProvider [][]domain.SearchResult) [][]domain.SearchResult {
	capped := make([][]domain.SearchResult, len(perProvider))
	for i, group := range perProvider {
		capped[i] = group[:min(len(group), maxMergeResultsPerProvider)]
	}
	return capped
}

type matchKey struct {
	title    string
	subtitle string
	lead     string
}

func matchKeyOf(r domain.SearchResult) matchKey {
	key := matchKey{
		title:    matchName(r.Title),
		subtitle: matchName(r.Subtitle),
	}
	if names := creditNames(r.Subtitle); len(names) > 0 {
		key.lead = names[0]
	}
	return key
}

type keyedResult struct {
	result domain.SearchResult
	key    matchKey
}

func keyed(r domain.SearchResult) keyedResult {
	return keyedResult{result: r, key: matchKeyOf(r)}
}

func Merge(perProvider [][]domain.SearchResult) []Entity {
	perProvider = capPerProvider(perProvider)
	names := artistNameRulesOf(perProvider)
	entities := make([]Entity, 0)
	keys := make([]matchKey, 0)
	for _, group := range perProvider {
		for rank, c := range group {
			cProviders := providersOf(c)
			kc := keyed(c)
			i, tier, ok := findMergeTarget(entities, keys, kc, names)
			if !ok {
				entities = append(entities, newEntity(c, cProviders, rank))
				keys = append(keys, kc.key)
				continue
			}
			entities[i].Result = mergeInto(entities[i].Result, c, tier)
			keys[i] = matchKeyOf(entities[i].Result)
			recordBestRank(&entities[i], cProviders, rank)
		}
	}
	return entities
}

func newEntity(c domain.SearchResult, providers map[domain.ProviderName]bool, rank int) Entity {
	ranks := make(map[domain.ProviderName]int, len(providers))
	for p := range providers {
		ranks[p] = rank
	}
	return Entity{Result: c, BestRank: ranks}
}

func findMergeTarget(entities []Entity, keys []matchKey, c keyedResult, names artistNameRules) (int, domain.EntityResolutionTier, bool) {
	byName, found := 0, false
	for i := range entities {
		e := keyedResult{result: entities[i].Result, key: keys[i]}
		tier, ok := sameEntity(e, c)
		if !ok {
			continue
		}
		if c.result.Kind != domain.ResultKindArtist || tier != domain.EntityResolutionNone {
			return i, tier, true
		}
		if names.blockNameJoin(e, c) {
			continue
		}
		if !found || entities[i].Result.FanCount > entities[byName].Result.FanCount {
			byName, found = i, true
		}
	}
	return byName, domain.EntityResolutionNone, found
}

type artistNameRules struct {
	lesser map[string]bool
}

func artistNameRulesOf(perProvider [][]domain.SearchResult) artistNameRules {
	return artistNameRules{lesser: lesserSameNameArtists(perProvider)}
}

func (n artistNameRules) blockNameJoin(e, c keyedResult) bool {
	return n.lesser[artistSourceKey(c.result)] || n.lesser[artistSourceKey(e.result)]
}

func lesserSameNameArtists(perProvider [][]domain.SearchResult) map[string]bool {
	lesser := make(map[string]bool)
	for _, group := range perProvider {
		lead := make(map[string]domain.SearchResult)
		for _, r := range group {
			if r.Kind != domain.ResultKindArtist || len(r.Sources) == 0 {
				continue
			}
			name := matchName(r.Title)
			best, seen := lead[name]
			if seen && r.FanCount <= best.FanCount {
				lesser[artistSourceKey(r)] = true
				continue
			}
			if seen {
				lesser[artistSourceKey(best)] = true
			}
			lead[name] = r
		}
	}
	return lesser
}

func artistSourceKey(r domain.SearchResult) string {
	if len(r.Sources) == 0 {
		return ""
	}
	return albumgrouping.SourceKey(r.Sources[0].Provider, r.Sources[0].ExternalID)
}

func recordBestRank(e *Entity, providers map[domain.ProviderName]bool, rank int) {
	for p := range providers {
		if prev, exists := e.BestRank[p]; !exists || rank < prev {
			e.BestRank[p] = rank
		}
	}
}

func sameEntity(ke, kc keyedResult) (domain.EntityResolutionTier, bool) {
	e, c := ke.result, kc.result
	if e.Kind != c.Kind {
		return domain.EntityResolutionNone, false
	}
	if tier, ok := sameIdentity(e, c); ok {
		return tier, true
	}
	if differentVersions(e, c) {
		return domain.EntityResolutionNone, false
	}
	return domain.EntityResolutionNone, sameName(e.Kind, ke.key, kc.key)
}

func sameIdentity(e, c domain.SearchResult) (domain.EntityResolutionTier, bool) {
	if eISRC := songfields.CanonicalISRC(e.ISRC); eISRC != "" && eISRC == songfields.CanonicalISRC(c.ISRC) {
		return domain.EntityResolutionISRC, true
	}
	if e.Kind == domain.ResultKindAlbum && e.UPC != "" && e.UPC == c.UPC {
		return domain.EntityResolutionUPC, true
	}
	return domain.EntityResolutionNone, false
}

func hasConflictingISRCs(a, b domain.SearchResult) bool {
	aISRC, bISRC := songfields.CanonicalISRC(a.ISRC), songfields.CanonicalISRC(b.ISRC)
	return aISRC != "" && bISRC != "" && aISRC != bISRC
}

func differentVersions(a, b domain.SearchResult) bool {
	return hasConflictingISRCs(a, b) && !slices.Equal(titlemarkers.Labels(a.Title), titlemarkers.Labels(b.Title))
}

func sameName(kind domain.ResultKind, e, c matchKey) bool {
	if kind == domain.ResultKindArtist {
		return e.title != "" && e.title == c.title
	}
	sameCredit := e.subtitle == c.subtitle || (e.lead != "" && e.lead == c.lead)
	return sameCredit && e.title != "" && e.title == c.title
}

func mergeInto(canonical, other domain.SearchResult, matchTier domain.EntityResolutionTier) domain.SearchResult {
	canonical, other = orderByCompleteness(canonical, other)
	tier := effectiveTier(canonical, other, matchTier)
	sources := albumgrouping.UnionSources(canonical.Sources, other.Sources)
	imageURL, imageSource := mergedArtwork(canonical, other, tier)
	merged := domain.SearchResult{
		Kind:           canonical.Kind,
		Title:          canonical.Title,
		Subtitle:       mergedCredit(canonical, other),
		ImageURL:       imageURL,
		ArtworkSource:  imageSource,
		Confidence:     confidenceFor(tier, len(sources)),
		Sources:        sources,
		Popularity:     math.Max(canonical.Popularity, other.Popularity),
		Extras:         mergeExtras(canonical, other),
		ResolutionTier: domain.StampResolutionTier(tier),
	}
	carryIdentity(&merged, canonical, other)
	merged.Explicit = mergedExplicit(canonical.Explicit, other.Explicit)
	alignExplicitWithISRC(&merged, canonical, other)
	carryFields(&merged, canonical, other)
	return merged
}

func mergedCredit(canonical, other domain.SearchResult) string {
	if onlyFromSoundCloud(canonical) && !onlyFromSoundCloud(other) && other.Subtitle != "" {
		return other.Subtitle
	}
	return canonical.Subtitle
}

func onlyFromSoundCloud(r domain.SearchResult) bool {
	provs := providersOf(r)
	return len(provs) == 1 && provs[domain.ProviderSoundCloud]
}

func orderByCompleteness(canonical, other domain.SearchResult) (domain.SearchResult, domain.SearchResult) {
	if completenessOf(other) > completenessOf(canonical) {
		return other, canonical
	}
	return canonical, other
}

func carryIdentity(merged *domain.SearchResult, canonical, other domain.SearchResult) {
	proven, hasProven := provenIdentity(canonical, other)
	identityAgrees := sameVersionIdentity(canonical, other)
	merged.ISRC = identityCarriedField(canonical.ISRC, other.ISRC, identityAgrees)
	if !identityAgrees && hasProven {
		merged.ISRC = proven.ISRC
	}
}

func mergedExplicit(a, b *bool) *bool {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return domain.BoolPtr(*a || *b)
}

func alignExplicitWithISRC(merged *domain.SearchResult, canonical, other domain.SearchResult) {
	if !hasConflictingISRCs(canonical, other) {
		return
	}
	source := canonical
	if songfields.CanonicalISRC(merged.ISRC) == songfields.CanonicalISRC(other.ISRC) {
		source = other
	}
	merged.Explicit = source.Explicit
}

func firstKnownPosition(a, b domain.TrackPosition) domain.TrackPosition {
	if a == (domain.TrackPosition{}) {
		return b
	}
	return a
}

func carryFields(merged *domain.SearchResult, canonical, other domain.SearchResult) {
	merged.RecordType = albumgrouping.FirstNonEmpty(canonical.RecordType, other.RecordType)
	merged.UPC = albumgrouping.FirstNonEmpty(canonical.UPC, other.UPC)
	merged.ArtworkPath = albumgrouping.FirstNonEmpty(canonical.ArtworkPath, other.ArtworkPath)
	merged.TrackPosition = firstKnownPosition(canonical.TrackPosition, other.TrackPosition)
	merged.Contributors = canonical.Contributors
	if len(merged.Contributors) == 0 {
		merged.Contributors = other.Contributors
	}
	merged.Year = albumgrouping.FirstNonZero(canonical.Year, other.Year)
	merged.ReleaseDate = albumgrouping.FirstNonEmpty(canonical.ReleaseDate, other.ReleaseDate)
	merged.TrackCount = albumgrouping.FirstNonZero(canonical.TrackCount, other.TrackCount)
	merged.ProviderRank = max(canonical.ProviderRank, other.ProviderRank)
	merged.FanCount = max(canonical.FanCount, other.FanCount)
	merged.Album = albumgrouping.FirstNonEmpty(canonical.Album, other.Album)
	merged.Duration = albumgrouping.FirstNonZero(canonical.Duration, other.Duration)
	merged.DeezerAlbumID = albumgrouping.FirstNonEmpty(canonical.DeezerAlbumID, other.DeezerAlbumID)
}

func mergeExtras(canonical, other domain.SearchResult) map[string]any {
	extras := make(map[string]any, len(canonical.Extras)+len(other.Extras))
	for k, v := range other.Extras {
		extras[k] = v
	}
	for k, v := range canonical.Extras {
		if v != nil || extras[k] == nil {
			extras[k] = v
		}
	}
	return extras
}

func effectiveTier(canonical, other domain.SearchResult, matchTier domain.EntityResolutionTier) domain.EntityResolutionTier {
	prev := canonical.ResolutionTier
	if !prev.Stamped {
		prev = other.ResolutionTier
	}
	return max(prev.Tier, matchTier)
}

func confidenceFor(tier domain.EntityResolutionTier, sourceCount int) domain.Confidence {
	switch tier {
	case domain.EntityResolutionISRC, domain.EntityResolutionUPC:
		return domain.ConfidenceHigh
	case domain.EntityResolutionNone, domain.EntityResolutionMBID, domain.EntityResolutionBridge:
	}
	if sourceCount > 1 {
		return domain.ConfidenceMedium
	}
	return domain.ConfidenceLow
}

func provenIdentity(a, b domain.SearchResult) (proven domain.SearchResult, ok bool) {
	aProven := a.ResolutionTier.Stamped && a.ResolutionTier.Tier != domain.EntityResolutionNone
	bProven := b.ResolutionTier.Stamped && b.ResolutionTier.Tier != domain.EntityResolutionNone
	switch {
	case aProven && !bProven:
		return a, true
	case bProven && !aProven:
		return b, true
	default:
		return domain.SearchResult{}, false
	}
}

func identityCarriedField(canonicalVal, otherVal string, identityAgrees bool) string {
	if !identityAgrees {
		return canonicalVal
	}
	return albumgrouping.FirstNonEmpty(canonicalVal, otherVal)
}

func sameVersionIdentity(a, b domain.SearchResult) bool {
	title := textnorm.NormalizeForIdentity(a.Title)
	if title == "" || title != textnorm.NormalizeForIdentity(b.Title) {
		return false
	}
	if a.Duration == 0 || b.Duration == 0 {
		return true
	}
	tolerance := math.Max(5, 0.03*math.Max(float64(a.Duration), float64(b.Duration)))
	return math.Abs(float64(a.Duration-b.Duration)) <= tolerance
}

func completenessOf(r domain.SearchResult) int {
	n := 0
	if r.ImageURL != "" {
		n++
	}
	if r.ISRC != "" {
		n++
	}
	if r.UPC != "" {
		n++
	}
	if r.Duration != 0 {
		n++
	}
	if r.Album != "" {
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

func mergedArtwork(canonical, other domain.SearchResult, tier domain.EntityResolutionTier) (url string, source domain.ProviderKey) {
	if tier == domain.EntityResolutionNone {
		switch {
		case canonical.HasArtworkIdentity() && !other.HasArtworkIdentity():
			return canonical.ImageURL, canonical.ArtworkSource
		case other.HasArtworkIdentity() && !canonical.HasArtworkIdentity():
			return other.ImageURL, other.ArtworkSource
		}
	}
	if other.ArtworkConfidenceRank() > canonical.ArtworkConfidenceRank() {
		return other.ImageURL, other.ArtworkSource
	}
	return domain.FirstNonEmptyArtwork(canonical, other)
}
