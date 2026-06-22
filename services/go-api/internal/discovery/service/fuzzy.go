package service

import (
	"altune/go-api/internal/shared/textnorm"
)

// levenshteinDistance delegates to the shared textnorm package.
// Kept here for the in-package correction caller.
func levenshteinDistance(s1, s2 string) int {
	return textnorm.LevenshteinDistance(s1, s2)
}
