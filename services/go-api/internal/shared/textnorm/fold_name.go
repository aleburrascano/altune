package textnorm

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// FoldName is the identity fold for an artist name: it unifies Unicode form
// (NFKC), collapses whitespace runs, and lowercases, but keeps diacritics and
// punctuation. It is deliberately milder than NormalizeForMatch so a persisted
// key built from it never merges distinct names like "Beyoncé" and "Beyonce".
// NFKC is the identity on ASCII and on NFC text without compatibility
// characters, so keys for such names are unchanged from the pre-NFKC fold.
func FoldName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(s)), " "))
}
