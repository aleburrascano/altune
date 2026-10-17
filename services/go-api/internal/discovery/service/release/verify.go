package release

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

const (
	AnchorMinReleaseGroups = 5
	mbVerifyMinTitles      = 4
	mbVerifyMinOverlap     = 4
	mbVerifyMinRatio       = 0.25
)

func FilterGroupsByMBAnchor(mbTitles map[string]bool, groups []ReleaseGroup) []ReleaseGroup {
	if len(mbTitles) < AnchorMinReleaseGroups {
		return groups
	}
	out := make([]ReleaseGroup, 0, len(groups))
	for _, g := range groups {
		if MatchesAnchor(g, mbTitles) {
			out = append(out, keepAnchoredReleases(g, mbTitles))
		}
	}
	return out
}

func keepAnchoredReleases(g ReleaseGroup, mbTitles map[string]bool) ReleaseGroup {
	if !IsMixedPage(g, mbTitles) {
		return g
	}
	_, overlap := anchorOverlap(g, mbTitles)
	kept := make([]domain.SearchResult, 0, overlap)
	for _, r := range g.Releases {
		if mbTitles[textnorm.NormalizeForMatch(r.Title)] {
			kept = append(kept, r)
		}
	}
	g.Releases = kept
	return g
}

func IsMixedPage(g ReleaseGroup, mbTitles map[string]bool) bool {
	titles, overlap := anchorOverlap(g, mbTitles)
	return len(titles) >= mbVerifyMinTitles && overlapRatio(overlap, len(titles)) < mbVerifyMinRatio
}

func FilterMixedPageTracks(mbTitles map[string]bool, albumGroups, trackGroups []ReleaseGroup) []ReleaseGroup {
	if len(mbTitles) < AnchorMinReleaseGroups {
		return trackGroups
	}
	mixed := mixedProviders(mbTitles, albumGroups)
	if len(mixed) == 0 {
		return trackGroups
	}
	listedBy := providersByTrackTitle(trackGroups)
	out := make([]ReleaseGroup, 0, len(trackGroups))
	for _, g := range trackGroups {
		kept := make([]domain.SearchResult, 0, len(g.Releases))
		for _, r := range g.Releases {
			if keepsMixedPageTrack(r, mbTitles, mixed, listedBy) {
				kept = append(kept, r)
			}
		}
		g.Releases = kept
		out = append(out, g)
	}
	return out
}

func keepsMixedPageTrack(r domain.SearchResult, mbTitles map[string]bool, mixed map[domain.ProviderName]bool, listedBy map[string]map[domain.ProviderName]bool) bool {
	p := resultProvider(r)
	return !mixed[p] || mbTitles[textnorm.NormalizeForMatch(r.Album)] || listedElsewhere(listedBy[textnorm.NormalizeForMatch(r.Title)], p)
}

func mixedProviders(mbTitles map[string]bool, albumGroups []ReleaseGroup) map[domain.ProviderName]bool {
	mixed := make(map[domain.ProviderName]bool)
	for _, g := range albumGroups {
		if MatchesAnchor(g, mbTitles) && IsMixedPage(g, mbTitles) {
			for _, r := range g.Releases {
				mixed[resultProvider(r)] = true
			}
		}
	}
	return mixed
}

func providersByTrackTitle(trackGroups []ReleaseGroup) map[string]map[domain.ProviderName]bool {
	listedBy := make(map[string]map[domain.ProviderName]bool)
	for _, g := range trackGroups {
		for _, r := range g.Releases {
			k := textnorm.NormalizeForMatch(r.Title)
			if listedBy[k] == nil {
				listedBy[k] = make(map[domain.ProviderName]bool)
			}
			listedBy[k][resultProvider(r)] = true
		}
	}
	return listedBy
}

func resultProvider(r domain.SearchResult) domain.ProviderName {
	if len(r.Sources) == 0 {
		return domain.ProviderUnknown
	}
	return r.Sources[0].Provider
}

func listedElsewhere(providers map[domain.ProviderName]bool, self domain.ProviderName) bool {
	for p := range providers {
		if p != self {
			return true
		}
	}
	return false
}

func overlapRatio(overlap, total int) float64 {
	return float64(overlap) / float64(total)
}

func anchorOverlap(g ReleaseGroup, mbTitles map[string]bool) (map[string]bool, int) {
	titles := make(map[string]bool, len(g.Releases))
	for _, r := range g.Releases {
		if k := textnorm.NormalizeForMatch(r.Title); k != "" {
			titles[k] = true
		}
	}
	overlap := 0
	for k := range titles {
		if mbTitles[k] {
			overlap++
		}
	}
	return titles, overlap
}

func MatchesAnchor(g ReleaseGroup, mbTitles map[string]bool) bool {
	titles, overlap := anchorOverlap(g, mbTitles)
	if len(titles) < mbVerifyMinTitles {
		return true
	}
	return overlap >= mbVerifyMinOverlap || overlapRatio(overlap, len(titles)) >= mbVerifyMinRatio
}

func NormalizeTitleSet(titles []string) map[string]bool {
	set := make(map[string]bool, len(titles))
	for _, t := range titles {
		if k := textnorm.NormalizeForMatch(t); k != "" {
			set[k] = true
		}
	}
	return set
}
