package textnorm

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	bracketSuffixRe = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	whitespaceRe    = regexp.MustCompile(`\s+`)
)

// NormalizeForMatch canonicalizes a music title/name for comparison:
//
//	NFKC → lowercase → strip diacritics → drop bracketed segments → & → "and"
//	→ strip apostrophes/periods/commas → strip symbols (keeping letters of any
//	script, incl. CJK) → collapse whitespace.
//
// AIDEV-DECISION (2026-06-21): removed two hand-curated, query-fit word lists —
// the leading-article strip (the/los/les/el/la/le, fit to expected catalog
// languages) and the feature-token normalization (feat/ft/featuring/with, whose
// "with" rule also mangled real titles like "Stuck with U"). Both sides of a
// comparison keep their articles now, so matching is unaffected.
//
// AIDEV-NOTE (2026-06-27): symbol stripping is via `stripSymbols` (Unicode-aware),
// NOT the old `[^\w\s]` regex. Go's `\w` is ASCII-only, so the regex deleted every
// CJK / non-Latin LETTER — a title like "坂本龍一" normalized to "" and became
// unrankable (qnorm empty → relevance disabled). stripSymbols keeps letters of any
// script while still dropping symbols AND hyphens, so the deferred "keep symbols"
// change (the artist "¥$"; the "07-The Best …" hyphen-tokenization trap) is
// unaffected — that remains an eval-matcher concern, fixed in the matcher, not here.
// Matching is symmetric (query and title use this same function), so keeping more
// letters cannot break a match; for ASCII input the output is byte-identical.
func NormalizeForMatch(text string) string {
	s := norm.NFKC.String(text)
	s = strings.ToLower(s)
	s = stripDiacritics(s)
	s = bracketSuffixRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = stripApostrophes(s)
	s = stripSymbols(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// stripSymbols replaces every rune that is not a letter, number, underscore, or
// whitespace with a space — keeping word content and dropping punctuation/symbols.
// For ASCII input the result is byte-identical to the old `[^\w\s]` regex (ASCII
// alnum and `_` kept, everything else → space); the only behavioral change is that
// non-ASCII LETTERS and NUMBERS (CJK, Cyrillic, Greek, æ/ø/…) now survive instead
// of being deleted. Hyphens and symbols are still dropped.
func stripSymbols(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 128:
			// ASCII: match the old `[^\w\s]` regex exactly — keep alnum, `_`, and
			// the ASCII whitespace `\s` recognized ([\t\n\f\r ]); else → space.
			switch {
			case (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_':
				b.WriteRune(r)
			case r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r':
				b.WriteRune(r)
			default:
				b.WriteByte(' ')
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func stripDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	// Recompose to canonical NFC: dropping the combining marks leaves Latin bases
	// uncombined (a no-op), but decomposed scripts like Hangul recompose from jamo
	// back to syllables, so the output is canonical rather than jamo-decomposed.
	return norm.NFC.String(b.String())
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
