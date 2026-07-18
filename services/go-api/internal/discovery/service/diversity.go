package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// Result-list shaping rules carried forward from the v1 ranking pipeline. These
// are orthogonal to ranking proper (the rebuilt Merge/Rank own ordering) — they
// cap per-artist repetition and fold same-name artist duplicates after ranking.
//
// AIDEV-NOTE: diversityWindow/maxPerArtistInTop are PRODUCT POLICY, not the
// query-fit ranking constants the rebuild purged (relevance bands, dominance
// windows, intent thresholds — see search.go's doctrine). They fit the product
// (a household of diverse tastes should not see one artist dominate the top
// results), not the query, so they are intentionally exempt from that purge.
// Changing them is a UX decision; validate against the top-K library eval
// (cmd/discoveryeval) since it shifts what users see at the top.
const (
	diversityWindow   = 10
	maxPerArtistInTop = 3
)

// EnforceDiversity limits the number of results per artist within the
// top diversityWindow positions to maxPerArtistInTop, moving overflow
// results below the window.
func EnforceDiversity(results []domain.SearchResult) []domain.SearchResult {
	// Clamp the window to the result count: a short list (≤ window) is still
	// entirely within the top positions, so the per-artist cap must apply there
	// too — early-returning would let one artist dominate a small result set.
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

// CollapseArtistDuplicates groups artist results that share the same normalized
// name. The highest-popularity artist is kept as the primary result. Remaining
// same-name artists are stored in a "collapsed_artists" extra on the primary.
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
		// Ambiguous name (multiple real artists share it): key by MBID so distinct
		// identities stay as separate cards instead of folding into one. Unambiguous
		// names collapse by name as before.
		if ambiguous[norm] {
			key = norm + "\x00" + r.MBID
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
		collapsedList := make([]map[string]any, len(g.otherIdxs))
		for j, idx := range g.otherIdxs {
			other := results[idx]
			// The collapsed entry's extras keep carrying mbid on the wire (it was an
			// Extras key before the typed-field promotion; clients key on it).
			otherExtras := copyExtras(other.Extras)
			if other.MBID != "" {
				otherExtras["mbid"] = other.MBID
			}
			collapsedList[j] = map[string]any{
				"title":    other.Title,
				"subtitle": other.Subtitle,
				"sources":  other.Sources,
				"extras":   otherExtras,
			}
			if other.ImageURL != "" {
				collapsedList[j]["image_url"] = other.ImageURL
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

// --- shared extras helpers (used by consensus, find_related, and the rules above) ---

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

// stringExtra and completenessOf (the merge.go helpers) are the single
// definitions; find_related and consensus call through to them.
