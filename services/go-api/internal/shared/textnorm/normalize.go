package textnorm

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

// NormalizeForMatch applies 8-step canonicalization for music title comparison:
// NFKC → lowercase → strip diacritics → normalize "feat" → drop brackets →
// strip leading article → & → "and" → strip punctuation → collapse whitespace.
func NormalizeForMatch(text string) string {
	s := norm.NFKC.String(text)
	s = strings.ToLower(s)
	s = stripDiacritics(s)
	s = featureTokenRe.ReplaceAllString(s, "feat")
	s = bracketSuffixRe.ReplaceAllString(s, " ")
	s = stripLeadingArticle(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = stripApostrophes(s)
	s = punctRe.ReplaceAllString(s, " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
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
