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
	titles, overlap := anchorOverlap(g, mbTitles)
	if len(titles) < mbVerifyMinTitles || overlapRatio(overlap, len(titles)) >= mbVerifyMinRatio {
		return g
	}
	kept := make([]domain.SearchResult, 0, overlap)
	for _, r := range g.Releases {
		if mbTitles[textnorm.NormalizeForMatch(r.Title)] {
			kept = append(kept, r)
		}
	}
	g.Releases = kept
	return g
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
