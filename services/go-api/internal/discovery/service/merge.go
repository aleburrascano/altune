package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/release"
	"altune/go-api/internal/shared/textnorm"
	"math"
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
}

func matchKeyOf(r domain.SearchResult) matchKey {
	return matchKey{
		title:    textnorm.NormalizeForMatch(r.Title),
		subtitle: textnorm.NormalizeForMatch(r.Subtitle),
	}
}

type keyedResult struct {
	result domain.SearchResult
	key    matchKey
}

func keyed(r domain.SearchResult) keyedResult {
	return keyedResult{result: r, key: matchKeyOf(r)}
}

func Merge(perProvider [][]domain.SearchResult) []Entity {
	return mergeObserving(perProvider, func() {})
}

func mergeObserving(perProvider [][]domain.SearchResult, onCompare func()) []Entity {
	perProvider = capPerProvider(perProvider)
	ambiguous := ambiguousArtistNames(perProvider)
	entities := make([]Entity, 0)
	keys := make([]matchKey, 0)
	for _, group := range perProvider {
		for rank, c := range group {
			cProviders := providersOf(c)
			kc := keyed(c)
			i, tier, ok := findMergeTarget(entities, keys, kc, ambiguous, onCompare)
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

func findMergeTarget(entities []Entity, keys []matchKey, c keyedResult, ambiguous map[string]bool, onCompare func()) (int, domain.EntityResolutionTier, bool) {
	for i := range entities {
		onCompare()
		tier, ok := sameEntity(keyedResult{result: entities[i].Result, key: keys[i]}, c)
		if !ok {
			continue
		}
		if c.result.Kind == domain.ResultKindArtist &&
			tier == domain.EntityResolutionNone &&
			ambiguous[c.key.title] {
			continue
		}
		return i, tier, true
	}
	return 0, domain.EntityResolutionNone, false
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
	if tier, ok, decided := sameIdentity(e, c); decided {
		return tier, ok
	}
	return domain.EntityResolutionNone, sameName(e.Kind, ke.key, kc.key)
}

func sameIdentity(e, c domain.SearchResult) (domain.EntityResolutionTier, bool, bool) {
	if e.ISRC != "" && c.ISRC != "" && e.ISRC == c.ISRC {
		return domain.EntityResolutionISRC, true, true
	}
	if e.Kind == domain.ResultKindAlbum && e.UPC != "" && c.UPC != "" && e.UPC == c.UPC &&
		(e.MBID == "" || c.MBID == "" || e.MBID == c.MBID) {
		return domain.EntityResolutionUPC, true, true
	}
	if e.MBID != "" && c.MBID != "" {
		if e.MBID == c.MBID {
			return domain.EntityResolutionMBID, true, true
		}
		return domain.EntityResolutionNone, false, true
	}
	if bridgeMatch(e, c) {
		return domain.EntityResolutionBridge, true, true
	}
	return domain.EntityResolutionNone, false, false
}

func sameName(kind domain.ResultKind, e, c matchKey) bool {
	if kind == domain.ResultKindArtist {
		return e.title != "" && e.title == c.title
	}
	return e.subtitle == c.subtitle && e.title != "" && e.title == c.title
}

func mergeInto(canonical, other domain.SearchResult, matchTier domain.EntityResolutionTier) domain.SearchResult {
	canonical, other = orderByCompleteness(canonical, other)
	tier := effectiveTier(canonical, other, matchTier)
	sources := release.UnionSources(canonical.Sources, other.Sources)
	imageURL, imageSource := mergedArtwork(canonical, other, tier)
	merged := domain.SearchResult{
		Kind:           canonical.Kind,
		Title:          canonical.Title,
		Subtitle:       canonical.Subtitle,
		ImageURL:       imageURL,
		ArtworkSource:  imageSource,
		Confidence:     confidenceFor(tier, len(sources)),
		Sources:        sources,
		Popularity:     math.Max(canonical.Popularity, other.Popularity),
		Extras:         mergeExtras(canonical, other),
		ResolutionTier: domain.StampResolutionTier(tier),
	}
	carryIdentity(&merged, canonical, other, matchTier)
	carryFields(&merged, canonical, other)
	return merged
}

func orderByCompleteness(canonical, other domain.SearchResult) (domain.SearchResult, domain.SearchResult) {
	if completenessOf(other) > completenessOf(canonical) {
		return other, canonical
	}
	return canonical, other
}

func carryIdentity(merged *domain.SearchResult, canonical, other domain.SearchResult, matchTier domain.EntityResolutionTier) {
	provenISRC, provenMBID, hasProven := provenIdentity(canonical, other)
	titleTierMerge := matchTier == domain.EntityResolutionNone && canonical.Kind != domain.ResultKindArtist
	identityAgrees := !titleTierMerge || sameVersionIdentity(canonical, other)
	merged.ISRC = identityCarriedField(canonical.ISRC, other.ISRC, identityAgrees)
	merged.MBID = identityCarriedField(canonical.MBID, other.MBID, identityAgrees)
	if !identityAgrees && hasProven {
		merged.ISRC = provenISRC
		merged.MBID = provenMBID
	}
}

func carryFields(merged *domain.SearchResult, canonical, other domain.SearchResult) {
	merged.RecordType = firstNonEmpty(canonical.RecordType, other.RecordType)
	merged.UPC = firstNonEmpty(canonical.UPC, other.UPC)
	merged.Xref = canonical.Xref
	if len(merged.Xref) == 0 {
		merged.Xref = other.Xref
	}
	merged.Year = firstNonZero(canonical.Year, other.Year)
	merged.ReleaseDate = firstNonEmpty(canonical.ReleaseDate, other.ReleaseDate)
	merged.TrackCount = firstNonZero(canonical.TrackCount, other.TrackCount)
	merged.ProviderRank = firstNonZero(canonical.ProviderRank, other.ProviderRank)
	merged.FanCount = firstNonZero(canonical.FanCount, other.FanCount)
	merged.Album = firstNonEmpty(canonical.Album, other.Album)
	merged.Duration = firstNonZero(canonical.Duration, other.Duration)
	merged.DeezerAlbumID = firstNonEmpty(canonical.DeezerAlbumID, other.DeezerAlbumID)
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
	case domain.EntityResolutionISRC, domain.EntityResolutionUPC, domain.EntityResolutionMBID, domain.EntityResolutionBridge:
		return domain.ConfidenceHigh
	}
	if sourceCount > 1 {
		return domain.ConfidenceMedium
	}
	return domain.ConfidenceLow
}

func firstNonEmpty[T ~string](a, b T) T {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero[T int | int64](a, b T) T {
	if a != 0 {
		return a
	}
	return b
}

func provenIdentity(a, b domain.SearchResult) (isrc, mbid string, ok bool) {
	aProven := a.ResolutionTier.Stamped && a.ResolutionTier.Tier != domain.EntityResolutionNone
	bProven := b.ResolutionTier.Stamped && b.ResolutionTier.Tier != domain.EntityResolutionNone
	switch {
	case aProven && !bProven:
		return a.ISRC, a.MBID, true
	case bProven && !aProven:
		return b.ISRC, b.MBID, true
	default:
		return "", "", false
	}
}

func identityCarriedField(canonicalVal, otherVal string, identityAgrees bool) string {
	if !identityAgrees {
		return canonicalVal
	}
	return firstNonEmpty(canonicalVal, otherVal)
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

func bridgeMatch(e, c domain.SearchResult) bool {
	if len(e.Xref) == 0 && len(c.Xref) == 0 {
		return false
	}
	ec := identityClaims(e)
	if len(ec) == 0 {
		return false
	}
	for claim := range identityClaims(c) {
		if ec[claim] {
			return true
		}
	}
	return false
}

func identityClaims(r domain.SearchResult) map[string]bool {
	claims := make(map[string]bool, len(r.Sources)+1)
	for _, s := range r.Sources {
		if s.ExternalID != "" {
			claims[release.SourceKey(s.Provider, s.ExternalID)] = true
		}
	}
	for name, id := range r.Xref {
		if id == "" {
			continue
		}
		if p, ok := name.ProviderName(); ok {
			claims[release.SourceKey(p, id)] = true
		}
	}
	return claims
}

func ambiguousArtistNames(perProvider [][]domain.SearchResult) map[string]bool {
	var flat []domain.SearchResult
	for _, group := range perProvider {
		flat = append(flat, group...)
	}
	return ambiguousArtistNamesFlat(flat)
}

func ambiguousArtistNamesFlat(results []domain.SearchResult) map[string]bool {
	mbidsByName := make(map[string]map[string]bool)
	for _, r := range results {
		if r.Kind != domain.ResultKindArtist || r.MBID == "" {
			continue
		}
		if !providersOf(r)[domain.ProviderMusicBrainz] {
			continue
		}
		name := textnorm.NormalizeForMatch(r.Title)
		if mbidsByName[name] == nil {
			mbidsByName[name] = make(map[string]bool)
		}
		mbidsByName[name][r.MBID] = true
	}
	ambiguous := make(map[string]bool)
	for name, mbids := range mbidsByName {
		if len(mbids) >= 2 {
			ambiguous[name] = true
		}
	}
	return ambiguous
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
