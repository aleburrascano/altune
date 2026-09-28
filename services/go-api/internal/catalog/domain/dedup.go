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
