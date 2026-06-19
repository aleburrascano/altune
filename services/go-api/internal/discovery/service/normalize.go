package service

import (
	"altune/go-api/internal/shared/textnorm"
)

// NormalizeForMatch delegates to the shared textnorm package.
// Kept here to avoid updating all callers in the service layer.
func NormalizeForMatch(text string) string {
	return textnorm.NormalizeForMatch(text)
}
