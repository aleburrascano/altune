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

func sortAlbumsByReleaseDateDesc(results []domain.SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		ki, kj := albumReleaseSortKey(results[i]), albumReleaseSortKey(results[j])
		if ki == "" || kj == "" {
			return ki != "" && kj == ""
		}
		return ki > kj
	})
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
