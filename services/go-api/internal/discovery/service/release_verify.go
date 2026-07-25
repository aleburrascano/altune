package service

import "altune/go-api/internal/shared/textnorm"

const (
	mbAnchorMinReleaseGroups = 5
	mbVerifyMinTitles        = 4
	mbVerifyMinOverlap       = 4
	mbVerifyMinRatio         = 0.25
)

func FilterGroupsByMBAnchor(mbTitles map[string]bool, groups []ReleaseGroup) []ReleaseGroup {
	if len(mbTitles) < mbAnchorMinReleaseGroups {
		return groups
	}
	out := make([]ReleaseGroup, 0, len(groups))
	for _, g := range groups {
		if groupMatchesAnchor(g, mbTitles) {
			out = append(out, g)
		}
	}
	return out
}

func groupMatchesAnchor(g ReleaseGroup, mbTitles map[string]bool) bool {
	titles := make(map[string]bool, len(g.Releases))
	for _, r := range g.Releases {
		if k := textnorm.NormalizeForMatch(r.Title); k != "" {
			titles[k] = true
		}
	}
	if len(titles) < mbVerifyMinTitles {
		return true
	}
	overlap := 0
	for k := range titles {
		if mbTitles[k] {
			overlap++
		}
	}
	return overlap >= mbVerifyMinOverlap || float64(overlap)/float64(len(titles)) >= mbVerifyMinRatio
}

func normalizeTitleSet(titles []string) map[string]bool {
	set := make(map[string]bool, len(titles))
	for _, t := range titles {
		if k := textnorm.NormalizeForMatch(t); k != "" {
			set[k] = true
		}
	}
	return set
}
