package phonetics

import (
	"strings"
	"unicode"
)

const maxCodeLen = 4

func DoubleMetaphone(s string) (primary, alternate string) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "", ""
	}

	var pri, alt strings.Builder
	add := func(code string) {
		if pri.Len() < maxCodeLen {
			pri.WriteString(code)
		}
		if alt.Len() < maxCodeLen {
			alt.WriteString(code)
		}
	}

	runes := []rune(s)
	cur := cursor{runes: runes, pos: posAfterSilentInitial(runes)}
	for cur.pos < len(runes) && pri.Len() < maxCodeLen {
		code, next := codeAt(cur)
		add(code)
		cur.pos = next
	}

	return pri.String(), alt.String()
}

// cursor is a letter of the upper-cased word plus the letters around it; every
// coding rule below is expressed as an offset from the letter being coded.
type cursor struct {
	runes []rune
	pos   int
}

func (c cursor) at(offset int) rune {
	i := c.pos + offset
	if i < 0 || i >= len(c.runes) {
		return 0
	}
	return c.runes[i]
}

func (c cursor) isWordStart() bool { return c.pos == 0 }

func (c cursor) endsWordAt(offset int) bool { return c.pos+offset >= len(c.runes) }

func posAfterSilentInitial(runes []rune) int {
	start := cursor{runes: runes}
	switch start.at(0) {
	case 'G', 'K', 'P':
		if start.at(1) == 'N' {
			return 1
		}
	case 'A':
		if start.at(1) == 'E' {
			return 1
		}
	case 'W':
		if start.at(1) == 'R' {
			return 1
		}
	}
	return 0
}

func codeAt(cur cursor) (code string, next int) {
	letter := cur.at(0)
	if isVowel(letter) {
		return vowelCode(cur), cur.pos + 1
	}

	if code, hasOwnCode := doubledLetterCode(letter); hasOwnCode {
		return code, posAfterDouble(cur)
	}

	switch letter {
	case 'C':
		return codeForC(cur)
	case 'D':
		return codeForD(cur)
	case 'G':
		return codeForG(cur)
	case 'H':
		return codeForH(cur)
	case 'P':
		return codeForP(cur)
	case 'S':
		return codeForS(cur)
	case 'T':
		return codeForT(cur)
	case 'W':
		return codeForW(cur)
	default:
		return "", cur.pos + 1
	}
}

// doubledLetterCode covers the letters whose whole rule is "emit this, and a
// doubled one still emits it once".
func doubledLetterCode(letter rune) (string, bool) {
	switch letter {
	case 'B':
		return "P", true
	case 'F', 'V':
		return "F", true
	case 'J':
		return "J", true
	case 'K', 'Q':
		return "K", true
	case 'L':
		return "L", true
	case 'M':
		return "M", true
	case 'N':
		return "N", true
	case 'R':
		return "R", true
	case 'X':
		return "KS", true
	case 'Z':
		return "S", true
	}
	return "", false
}

func vowelCode(cur cursor) string {
	if cur.isWordStart() {
		return "A"
	}
	return ""
}

func codeForC(cur cursor) (string, int) {
	switch {
	case cur.at(1) == 'H':
		return "X", cur.pos + 2
	case isFrontVowel(cur.at(1)):
		return "S", cur.pos + 2
	case mergesAfterC(cur.at(1)):
		return "K", cur.pos + 2
	default:
		return "K", cur.pos + 1
	}
}

func codeForD(cur cursor) (string, int) {
	if cur.at(1) == 'G' && isFrontVowel(cur.at(2)) {
		return "J", cur.pos + 2
	}
	return "T", posAfterDouble(cur)
}

func codeForG(cur cursor) (string, int) {
	switch {
	case cur.at(1) == 'H':
		return codeForGH(cur)
	case cur.at(1) == 'N':
		return codeForGN(cur)
	case cur.at(1) == 'G' || isVowel(cur.at(1)):
		return "K", cur.pos + 2
	default:
		return "", cur.pos + 1
	}
}

func codeForGH(cur cursor) (string, int) {
	if !cur.isWordStart() && !isVowel(cur.at(-1)) {
		return "", cur.pos + 2
	}
	return "K", cur.pos + 2
}

func codeForGN(cur cursor) (string, int) {
	afterGN := cur.pos + 2
	if !cur.endsWordAt(2) && !isVowel(cur.at(2)) {
		return "", afterGN
	}
	return "KN", afterGN
}

func codeForH(cur cursor) (string, int) {
	startsSyllable := cur.isWordStart() || isVowel(cur.at(-1))
	if isVowel(cur.at(1)) && startsSyllable {
		return "H", cur.pos + 2
	}
	return "", cur.pos + 1
}

func codeForP(cur cursor) (string, int) {
	if cur.at(1) == 'H' {
		return "F", cur.pos + 2
	}
	return "P", posAfterDouble(cur)
}

func codeForS(cur cursor) (string, int) {
	switch {
	case cur.at(1) == 'H':
		return "X", cur.pos + 2
	case cur.at(1) == 'C':
		return codeForSC(cur)
	case mergesAfterS(cur.at(1)):
		return "S", cur.pos + 2
	default:
		return "S", cur.pos + 1
	}
}

func codeForSC(cur cursor) (string, int) {
	switch {
	case cur.at(2) == 'H':
		return "SK", cur.pos + 3
	case isFrontVowel(cur.at(2)):
		return "S", cur.pos + 3
	default:
		return "SK", cur.pos + 2
	}
}

func codeForT(cur cursor) (string, int) {
	switch {
	case cur.at(1) == 'H':
		return "0", cur.pos + 2
	case startsSibilantTI(cur):
		return "X", cur.pos + 3
	default:
		return "T", posAfterDouble(cur)
	}
}

func codeForW(cur cursor) (string, int) {
	if isVowel(cur.at(1)) {
		return "A", cur.pos + 2
	}
	return "", cur.pos + 1
}

func posAfterDouble(cur cursor) int {
	if cur.at(1) == cur.at(0) {
		return cur.pos + 2
	}
	return cur.pos + 1
}

func startsSibilantTI(cur cursor) bool {
	return cur.at(1) == 'I' && (cur.at(2) == 'O' || cur.at(2) == 'A')
}

func isVowel(r rune) bool {
	switch r {
	case 'A', 'E', 'I', 'O', 'U', 'Y':
		return true
	}
	return false
}

func isFrontVowel(r rune) bool {
	switch r {
	case 'I', 'E', 'Y':
		return true
	}
	return false
}

func mergesAfterC(r rune) bool {
	switch r {
	case 'C', 'K', 'Q':
		return true
	}
	return false
}

func mergesAfterS(r rune) bool {
	switch r {
	case 'S', 'Z':
		return true
	}
	return false
}

func MetaphoneKey(term string) string {
	words := strings.Fields(term)
	if len(words) == 0 {
		return ""
	}
	var codes []string
	for _, w := range words {
		cleaned := stripNonAlpha(w)
		if cleaned == "" {
			continue
		}
		p, _ := DoubleMetaphone(cleaned)
		if p != "" {
			codes = append(codes, p)
		}
	}
	return strings.Join(codes, "")
}

func stripNonAlpha(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
