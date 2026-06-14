package service

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	bracketSuffixRe  = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	featureTokenRe   = regexp.MustCompile(`(?i)\b(feat\.?|ft\.?|featuring|with)\b`)
	leadingArticleRe = regexp.MustCompile(`(?i)^\s*(the|los|les|el|la|le)\s+`)
	punctRe          = regexp.MustCompile(`[^\w\s]`)
	whitespaceRe     = regexp.MustCompile(`\s+`)
)

// NormalizeForMatch applies the 8-step canonicalization matching Python's normalize_for_match.
func NormalizeForMatch(text string) string {
	// 1. NFKC
	s := norm.NFKC.String(text)
	// 2. Lowercase
	s = strings.ToLower(s)
	// 3. Strip diacritics
	s = stripDiacritics(s)
	// 5. Normalize feature notation BEFORE bracket-strip
	s = featureTokenRe.ReplaceAllString(s, "feat")
	// 4. Drop bracketed suffixes
	s = bracketSuffixRe.ReplaceAllString(s, " ")
	// 6. Strip leading article (only if ≥2 tokens remain)
	s = stripLeadingArticle(s)
	// 7. & → and; strip apostrophes/periods/commas; drop other punctuation; collapse whitespace
	s = strings.ReplaceAll(s, "&", " and ")
	s = stripApostrophes(s)
	s = punctRe.ReplaceAllString(s, " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
	// 8. Trim
	return strings.TrimSpace(s)
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

func stripLeadingArticle(s string) string {
	stripped := leadingArticleRe.ReplaceAllString(s, "")
	trimmed := strings.TrimSpace(stripped)
	if trimmed != "" && trimmed != strings.TrimSpace(s) {
		return stripped
	}
	return s
}

func stripApostrophes(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\'', '’', '.', ',':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
