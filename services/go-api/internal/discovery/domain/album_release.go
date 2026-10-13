package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"sort"
	"strconv"
)

func TitleSubtitleKey(title, subtitle string) string {
	return textnorm.NormalizeForMatch(title) + "|" + textnorm.NormalizeForMatch(subtitle)
}

func DedupAlbums(results []SearchResult) []SearchResult {
	seen := make(map[string]int)
	var deduped []SearchResult

	for _, r := range results {
		normTitle := TitleSubtitleKey(r.Title, r.Subtitle)
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

func SortByReleaseDateDesc[T any](items []T, key func(T) string) {
	pairs := normalizeReleaseSortKeys(items, key)
	sort.SliceStable(pairs, func(i, j int) bool { return releaseKeyNewer(pairs[i].key, pairs[j].key) })
	sorted := make([]T, 0, len(pairs))
	for _, p := range pairs {
		sorted = append(sorted, p.item)
	}
	copy(items, sorted)
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

func normalizeReleaseSortKeys[T any](items []T, key func(T) string) []keyedItem[T] {
	pairs := make([]keyedItem[T], 0, len(items))
	minLen := make(map[string]int)
	for _, it := range items {
		k := key(it)
		y := releaseKeyYear(k)
		if l, ok := minLen[y]; !ok || len(k) < l {
			minLen[y] = len(k)
		}
		pairs = append(pairs, keyedItem[T]{key: k, item: it})
	}
	for i := range pairs {
		pairs[i].key = pairs[i].key[:minLen[releaseKeyYear(pairs[i].key)]]
	}
	return pairs
}

func releaseKeyYear(k string) string {
	if len(k) > 4 {
		return k[:4]
	}
	return k
}

func AlbumReleaseSortKey(r SearchResult) string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	if r.Year > 0 {
		return strconv.Itoa(r.Year)
	}
	return ""
}

func NormalizeAlbumYears(results []SearchResult) {
	for i := range results {
		NormalizeReleaseYear(&results[i])
	}
}

func parseYear(s string) int {
	y, err := strconv.Atoi(s)
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

func NormalizeReleaseYear(r *SearchResult) {
	if r.Year != 0 || len(r.ReleaseDate) < 4 {
		return
	}
	if y := parseYear(r.ReleaseDate[:4]); y > 0 {
		r.Year = y
	}
}
