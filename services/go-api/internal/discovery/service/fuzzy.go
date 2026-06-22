package service

import (
	"altune/go-api/internal/shared/textnorm"
)

// TokenSortRatio delegates to the shared textnorm package.
// Kept here to avoid updating all callers in the service layer.
func TokenSortRatio(s1, s2 string) float64 {
	return textnorm.TokenSortRatio(s1, s2)
}

// levenshteinDistance delegates to the shared textnorm package.
// Kept here for the in-package correction caller.
func levenshteinDistance(s1, s2 string) int {
	return textnorm.LevenshteinDistance(s1, s2)
}
