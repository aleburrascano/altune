package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func computeDedupKey(title, artist, album string) string {
	parts := []string{
		normalizeForDedup(title),
		normalizeForDedup(artist),
		normalizeForDedup(album),
	}
	return strings.Join(parts, "|")
}

func normalizeForDedup(s string) string {
	s = strings.ToLower(canonicalizeField(s))
	s = stripNonAlphanumeric(s)
	return strings.Join(strings.Fields(s), " ")
}

// canonicalizeField is the display-preserving base that dedup's key computation
// builds on: it folds stray/internal whitespace and unifies Unicode form (NFKC)
// and trims, but keeps case and punctuation so stored values remain readable.
// Applying it to album/artist/album_artist at write time makes the library-lens
// grouping coalesce values dedup already treats as equivalent (e.g. "Abbey Road"
// and " Abbey Road"), instead of fragmenting them into separate groups.
func canonicalizeField(s string) string {
	s = norm.NFKC.String(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func stripNonAlphanumeric(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
