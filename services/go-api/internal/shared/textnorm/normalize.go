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

// KeySeparator joins the parts of a composite key. NormalizeForMatch strips it
// from text, so no part can contain one and no two splits of the same words can
// produce the same key.
const KeySeparator = "\x1f"

// NameKey is the key a name-keyed store is written and read under. Both sides
// must derive it here: a second derivation that drifts turns every lookup into a
// silent miss. It is empty when every part is, because a name that normalizes
// away identifies nothing and every such entity would share one entry.
func NameKey(parts ...string) string {
	normalized := make([]string, len(parts))
	hasIdentity := false
	for i, part := range parts {
		normalized[i] = NormalizeForMatch(part)
		hasIdentity = hasIdentity || normalized[i] != ""
	}
	if !hasIdentity {
		return ""
	}
	return strings.Join(normalized, KeySeparator)
}

func stripSymbols(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isWordContent(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func isWordContent(r rune) bool {
	if r < 128 {
		return isASCIIWordChar(r) || isASCIIWhitespace(r)
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isASCIIWordChar(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		r == '_'
}

func isASCIIWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
}

func stripDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	withoutCombiningMarks := b.String()
	return norm.NFC.String(withoutCombiningMarks)
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
