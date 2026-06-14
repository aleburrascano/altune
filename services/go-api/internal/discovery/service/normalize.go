package service

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	articleRe  = regexp.MustCompile(`(?i)\b(the|a|an)\b`)
	bracketRe  = regexp.MustCompile(`\([^)]*\)|\[[^\]]*\]`)
	featureRe  = regexp.MustCompile(`(?i)\b(feat\.?|ft\.?|featuring|with)\b.*`)
	punctRe    = regexp.MustCompile(`[^\p{L}\p{N}\s]`)
	multiSpace = regexp.MustCompile(`\s+`)
)

// NormalizeForMatch applies the 8-step canonicalization for fuzzy matching.
func NormalizeForMatch(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	s = stripDiacritics(s)
	s = articleRe.ReplaceAllString(s, "")
	s = bracketRe.ReplaceAllString(s, "")
	s = featureRe.ReplaceAllString(s, "")
	s = punctRe.ReplaceAllString(s, "")
	s = multiSpace.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

func stripDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
