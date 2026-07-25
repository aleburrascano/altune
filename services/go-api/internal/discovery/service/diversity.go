package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

const (
	diversityWindow   = 10
	maxPerArtistInTop = 3
)

func EnforceDiversity(results []domain.SearchResult) []domain.SearchResult {
	windowSize := diversityWindow
	if len(results) < windowSize {
		windowSize = len(results)
	}
	window := results[:windowSize]
	rest := results[windowSize:]

	artistCount := make(map[string]int)
	kept := make([]domain.SearchResult, 0, diversityWindow)
	overflow := make([]domain.SearchResult, 0)

	for _, r := range window {
		artist := textnorm.NormalizeForMatch(r.Subtitle)
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

func CollapseArtistDuplicates(results []domain.SearchResult) []domain.SearchResult {
	type group struct {
		primaryIdx int
		primaryPop float64
		otherIdxs  []int
	}
	ambiguous := ambiguousArtistNamesFlat(results)
	groups := make(map[string]*group)
	order := []string{}

	for i, r := range results {
		if r.Kind != domain.ResultKindArtist {
			continue
		}
		norm := textnorm.NormalizeForMatch(r.Title)
		key := norm
		if ambiguous[norm] {
			key = norm + "\x00" + r.MBID
			if r.MBID == "" && len(r.Sources) > 0 {
				key = norm + "\x00" + r.Sources[0].Provider.String() + ":" + r.Sources[0].ExternalID
			}
		}
		pop := r.Popularity
		g, exists := groups[key]
		if !exists {
			groups[key] = &group{primaryIdx: i, primaryPop: pop}
			order = append(order, key)
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
		collapsedList := make([]domain.CollapsedArtistSummary, len(g.otherIdxs))
		for j, idx := range g.otherIdxs {
			other := results[idx]
			otherExtras := copyExtras(other.Extras)
			if other.MBID != "" {
				otherExtras["mbid"] = other.MBID
			}
			collapsedList[j] = domain.CollapsedArtistSummary{
				Title:    other.Title,
				Subtitle: other.Subtitle,
				ImageURL: other.ImageURL,
				Sources:  other.Sources,
				Extras:   otherExtras,
			}
			remove[idx] = true
		}
		primary := &results[g.primaryIdx]
		extras := copyExtras(primary.Extras)
		extras["collapsed_artists"] = collapsedList
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
