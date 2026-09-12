package app

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"sort"
	"strconv"
	"strings"
)

func mergeAlbumSeeds(seeds []rawSeed) []domain.SearchResult {
	seen := map[string]int{}
	var out []domain.SearchResult
	for _, s := range okSeedItems(seeds) {
		key := textnorm.NormalizeForMatch(s.Title)
		if i, ok := seen[key]; ok {
			out[i] = mergeAlbumPair(out[i], s)
			continue
		}
		seen[key] = len(out)
		out = append(out, s)
	}
	sortReleasesByDateDesc(out)
	return out
}

func mergeTrackSeeds(seeds []rawSeed) []domain.SearchResult {
	seen := map[string]bool{}
	var out []domain.SearchResult
	for _, t := range okSeedItems(seeds) {
		key := textnorm.NormalizeForMatch(t.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func okSeedItems(seeds []rawSeed) []domain.SearchResult {
	var flat []domain.SearchResult
	for _, s := range seeds {
		if s.status == "ok" {
			flat = append(flat, s.items...)
		}
	}
	return flat
}

func mergeAlbumPair(existing, incoming domain.SearchResult) domain.SearchResult {
	winner := existing
	if incoming.TrackCount > existing.TrackCount {
		winner = incoming
	}
	winner.Sources = unionSourceRefs(existing.Sources, incoming.Sources)
	return winner
}

func unionSourceRefs(a, b []domain.SourceRef) []domain.SourceRef {
	seen := map[string]bool{}
	out := make([]domain.SourceRef, 0, len(a)+len(b))
	for _, s := range append(append([]domain.SourceRef{}, a...), b...) {
		key := s.Provider.String() + "|" + s.ExternalID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func sortReleasesByDateDesc(items []domain.SearchResult) {
	sort.SliceStable(items, func(i, j int) bool {
		return releaseKeyNewer(releaseSortKey(items[i]), releaseSortKey(items[j]))
	})
}

// releaseKeyNewer reports whether key ki sorts newer-first than kj. Keys are
// either a full ISO date or a bare year; it normalizes them to the same
// precision first, so a year-only value ties with a same-year full date
// instead of always sorting older than it.
func releaseKeyNewer(ki, kj string) bool {
	if ki == "" || kj == "" {
		return ki != "" && kj == ""
	}
	if len(ki) != len(kj) && releaseYear(ki) == releaseYear(kj) {
		return false
	}
	return ki > kj
}

func releaseYear(key string) string {
	if i := strings.IndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return key
}

func releaseSortKey(r domain.SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}
