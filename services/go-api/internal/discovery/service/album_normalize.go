package service

import (
	"sort"
	"strconv"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func dedupAlbums(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]int)
	var deduped []domain.SearchResult

	for _, r := range results {
		normTitle := textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
		if idx, ok := seen[normTitle]; ok {
			if r.TrackCount > deduped[idx].TrackCount {
				deduped[idx] = r
			}
			continue
		}
		seen[normTitle] = len(deduped)
		deduped = append(deduped, r)
	}
	return deduped
}

// sortByReleaseDateDesc orders items newest first with undated items last,
// keeping input order for ties. Keys may mix precisions ("2020" from a
// year-only provider vs "2020-01-01"); a raw string compare would always rank
// the bare year as older because it is a byte prefix. Each key is truncated
// to the coarsest precision present within its year before comparing, so
// same-year releases of differing precision tie instead of being misordered,
// and the comparison stays a strict weak ordering.
func sortByReleaseDateDesc[T any](items []T, key func(T) string) {
	pairs := make([]keyedItem[T], len(items))
	for i, k := range normalizeReleaseSortKeys(items, key) {
		pairs[i] = keyedItem[T]{key: k, item: items[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return releaseKeyNewer(pairs[i].key, pairs[j].key) })
	for i := range pairs {
		items[i] = pairs[i].item
	}
}

type keyedItem[T any] struct {
	key  string
	item T
}

func releaseKeyNewer(ki, kj string) bool {
	if ki == "" || kj == "" {
		return ki != "" && kj == ""
	}
	return ki > kj
}

// normalizeReleaseSortKeys returns each item's sort key truncated to the
// shortest key length seen among keys sharing the same year.
func normalizeReleaseSortKeys[T any](items []T, key func(T) string) []string {
	keys := make([]string, len(items))
	minLen := make(map[string]int)
	for i, it := range items {
		keys[i] = key(it)
		y := releaseKeyYear(keys[i])
		if l, ok := minLen[y]; !ok || len(keys[i]) < l {
			minLen[y] = len(keys[i])
		}
	}
	for i, k := range keys {
		keys[i] = k[:minLen[releaseKeyYear(k)]]
	}
	return keys
}

func releaseKeyYear(k string) string {
	if len(k) > 4 {
		return k[:4]
	}
	return k
}

func albumReleaseSortKey(r domain.SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}

func normalizeAlbumYears(results []domain.SearchResult) {
	for i := range results {
		normalizeReleaseYear(&results[i])
	}
}

func parseYear(s string) int {
	y, err := strconv.Atoi(s)
	if err != nil || y <= 0 {
		return 0
	}
	return y
}
