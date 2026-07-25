package service

import (
	"math"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

type Entity struct {
	Result   domain.SearchResult
	BestRank map[domain.ProviderName]int
}

func Merge(perProvider [][]domain.SearchResult) []Entity {
	ambiguous := ambiguousArtistNames(perProvider)
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
				if c.Kind == domain.ResultKindArtist &&
					tier == domain.EntityResolutionNone &&
					ambiguous[textnorm.NormalizeForMatch(c.Title)] {
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

func sameEntity(e, c domain.SearchResult) (domain.EntityResolutionTier, bool) {
	if e.Kind != c.Kind {
		return domain.EntityResolutionNone, false
	}

	if e.ISRC != "" && c.ISRC != "" && e.ISRC == c.ISRC {
		return domain.EntityResolutionISRC, true
	}
	if e.Kind == domain.ResultKindAlbum && e.UPC != "" && c.UPC != "" && e.UPC == c.UPC &&
		(e.MBID == "" || c.MBID == "" || e.MBID == c.MBID) {
		return domain.EntityResolutionUPC, true
	}
	if e.MBID != "" && c.MBID != "" {
		if e.MBID == c.MBID {
			return domain.EntityResolutionMBID, true
		}
		return domain.EntityResolutionNone, false
	}

	if bridgeMatch(e, c) {
		return domain.EntityResolutionBridge, true
	}

	if e.Kind == domain.ResultKindArtist {
		name := textnorm.NormalizeForMatch(e.Title)
		if name == "" {
			return domain.EntityResolutionNone, false
		}
		return domain.EntityResolutionNone, name == textnorm.NormalizeForMatch(c.Title)
	}

	if textnorm.NormalizeForMatch(e.Subtitle) != textnorm.NormalizeForMatch(c.Subtitle) {
		return domain.EntityResolutionNone, false
	}
	title := textnorm.NormalizeForMatch(e.Title)
	if title != "" && title == textnorm.NormalizeForMatch(c.Title) {
		return domain.EntityResolutionNone, true
	}
	return domain.EntityResolutionNone, false
}

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
	if prev := domain.ResolutionTierFromExtras(extras); prev > tier {
		tier = prev
	}
	extras["resolution_tier"] = tier.String()

	imageURL := canonical.ImageURL
	if imageURL == "" {
		imageURL = other.ImageURL
	}

	conf := domain.ConfidenceLow
	switch tier {
	case domain.EntityResolutionISRC, domain.EntityResolutionUPC, domain.EntityResolutionMBID, domain.EntityResolutionBridge:
		conf = domain.ConfidenceHigh
	default:
		if len(sources) > 1 {
			conf = domain.ConfidenceMedium
		}
	}

	merged := domain.SearchResult{
		Kind:       canonical.Kind,
		Title:      canonical.Title,
		Subtitle:   canonical.Subtitle,
		ImageURL:   imageURL,
		Confidence: conf,
		Sources:    sources,
		Popularity: math.Max(canonical.Popularity, other.Popularity),
		Extras:     extras,
	}
	merged.ISRC = firstNonEmpty(canonical.ISRC, other.ISRC)
	merged.UPC = firstNonEmpty(canonical.UPC, other.UPC)
	merged.MBID = firstNonEmpty(canonical.MBID, other.MBID)
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
	return merged
}

func firstNonEmpty(a, b string) string {
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

type providerID struct {
	provider domain.ProviderName
	id       string
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

func identityClaims(r domain.SearchResult) map[providerID]bool {
	claims := make(map[providerID]bool, len(r.Sources)+1)
	for _, s := range r.Sources {
		if s.ExternalID != "" {
			claims[providerID{s.Provider, s.ExternalID}] = true
		}
	}
	for name, id := range r.Xref {
		if id == "" {
			continue
		}
		if p, err := domain.ParseProviderName(name); err == nil {
			claims[providerID{p, id}] = true
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
