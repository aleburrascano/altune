package service

import (
	"log/slog"
	"strconv"

	"altune/go-api/internal/discovery/domain"
)

const birthYearOffset = 14

type DiscographyFilterInput struct {
	BirthYear      int
	MBConfirmed    map[string]bool
	DiscogsConfirmed map[string]bool
}

func FilterContamination(albums []domain.SearchResult, input DiscographyFilterInput) []domain.SearchResult {
	if len(albums) == 0 {
		return albums
	}

	hasMB := len(input.MBConfirmed) > 0
	hasDiscogs := len(input.DiscogsConfirmed) > 0

	kept := make([]domain.SearchResult, 0, len(albums))
	removedCount := 0

	for _, album := range albums {
		mismatch := 0
		titleNorm := NormalizeForMatch(album.Title)

		if input.BirthYear > 0 {
			albumYear := extractYear(album)
			if albumYear > 0 && albumYear < input.BirthYear+birthYearOffset {
				mismatch++
			}
		}

		inMB := hasMB && input.MBConfirmed[titleNorm]
		inDiscogs := hasDiscogs && input.DiscogsConfirmed[titleNorm]

		if inMB || inDiscogs {
			kept = append(kept, album)
			continue
		}

		if hasMB && !inMB {
			mismatch++
		}
		if hasDiscogs && !inDiscogs {
			mismatch++
		}

		if mismatch >= 2 {
			removedCount++
			slog.Debug("discography.filtered",
				"title", album.Title,
				"mismatch", mismatch,
				"reason", "contamination")
			continue
		}

		kept = append(kept, album)
	}

	if removedCount > 0 {
		slog.Info("discography.filter_applied",
			"input", len(albums),
			"kept", len(kept),
			"removed", removedCount,
		)
	}

	return kept
}

func extractYear(r domain.SearchResult) int {
	if r.Extras == nil {
		return 0
	}
	if v, ok := r.Extras["year"]; ok {
		switch y := v.(type) {
		case int:
			return y
		case float64:
			return int(y)
		case string:
			if n, err := strconv.Atoi(y); err == nil {
				return n
			}
		}
	}
	if v, ok := r.Extras["release_date"]; ok {
		if s, ok := v.(string); ok && len(s) >= 4 {
			if n, err := strconv.Atoi(s[:4]); err == nil {
				return n
			}
		}
	}
	return 0
}
