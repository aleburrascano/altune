package textnorm

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	bracketSuffixRe = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	punctRe         = regexp.MustCompile(`[^\w\s]`)
	whitespaceRe    = regexp.MustCompile(`\s+`)
)

// NormalizeForMatch canonicalizes a music title/name for comparison:
//
//	NFKC → lowercase → strip diacritics → drop bracketed segments → & → "and"
//	→ strip apostrophes/periods/commas → strip remaining punctuation → collapse
//	whitespace.
//
// AIDEV-DECISION (2026-06-21): removed two hand-curated, query-fit word lists —
// the leading-article strip (the/los/les/el/la/le, fit to expected catalog
// languages) and the feature-token normalization (feat/ft/featuring/with, whose
// "with" rule also mangled real titles like "Stuck with U"). Both sides of a
// comparison keep their articles now, so matching is unaffected.
//
// AIDEV-NOTE: `punctRe` (strip punctuation → space) is intentionally RETAINED.
// A proposed change to keep symbols — so symbol-only names like the artist "¥$"
// don't normalize to empty — was deferred: it also keeps hyphens, which glues
// separator quirks ("07-The Best …") into one token and breaks tokenized
// matching. The "¥$" case is an eval-matcher artifact (the pipeline returns the
// correct result); fix it in the matcher, not here. See the next-session handoff.
func NormalizeForMatch(text string) string {
	s := norm.NFKC.String(text)
	s = strings.ToLower(s)
	s = stripDiacritics(s)
	s = bracketSuffixRe.ReplaceAllString(s, " ")
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
