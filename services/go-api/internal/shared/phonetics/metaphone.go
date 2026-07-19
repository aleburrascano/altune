package phonetics

import (
	"strings"
	"unicode"
)

// DoubleMetaphone returns primary and alternate phonetic codes for a word.
// Simplified implementation covering the most common English pronunciation
// patterns relevant to music artist/track names.
func DoubleMetaphone(s string) (primary, alternate string) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "", ""
	}

	var pri, alt strings.Builder
	runes := []rune(s)
	n := len(runes)
	pos := 0
	maxLen := 4

	at := func(offset int) rune {
		i := pos + offset
		if i < 0 || i >= n {
			return 0
		}
		return runes[i]
	}

	isVowel := func(r rune) bool {
		switch r {
		case 'A', 'E', 'I', 'O', 'U', 'Y':
			return true
		}
		return false
	}

	addBoth := func(p, a string) {
		if pri.Len() < maxLen {
			pri.WriteString(p)
		}
		if alt.Len() < maxLen {
			alt.WriteString(a)
		}
	}
	add := func(c string) { addBoth(c, c) }

	// Skip initial silent letters
	switch at(0) {
	case 'G', 'K', 'P':
		if at(1) == 'N' {
			pos++
		}
	case 'A':
		if at(1) == 'E' {
			pos++
		}
	case 'W':
		if at(1) == 'R' {
			pos++
		}
	}

	for pos < n && pri.Len() < maxLen {
		c := at(0)

		if isVowel(c) {
			if pos == 0 {
				add("A")
			}
			pos++
			continue
		}

		switch c {
		case 'B':
			add("P")
			if at(1) == 'B' {
				pos += 2
			} else {
				pos++
			}

		case 'C':
			if at(1) == 'H' {
				add("X")
				pos += 2
			} else if at(1) == 'I' || at(1) == 'E' || at(1) == 'Y' {
				addBoth("S", "S")
				pos += 2
			} else {
				add("K")
				if at(1) == 'C' || at(1) == 'K' || at(1) == 'Q' {
					pos += 2
				} else {
					pos++
				}
			}

		case 'D':
			if at(1) == 'G' && (at(2) == 'I' || at(2) == 'E' || at(2) == 'Y') {
				add("J")
				pos += 2
			} else {
				add("T")
				if at(1) == 'D' {
					pos += 2
				} else {
					pos++
				}
			}

		case 'F':
			add("F")
			if at(1) == 'F' {
				pos += 2
			} else {
				pos++
			}

		case 'G':
			if at(1) == 'H' {
				if pos > 0 && !isVowel(at(-1)) {
					pos += 2
					continue
				}
				add("K")
				pos += 2
			} else if at(1) == 'N' {
				pos += 2
				if pos < n && !isVowel(at(0)) {
					continue
				}
				add("KN")
			} else if at(1) == 'G' {
				add("K")
				pos += 2
			} else if isVowel(at(1)) {
				add("K")
				pos += 2
			} else {
				pos++
			}

		case 'H':
			if isVowel(at(1)) && (pos == 0 || isVowel(at(-1))) {
				add("H")
				pos += 2
			} else {
				pos++
			}

		case 'J':
			add("J")
			if at(1) == 'J' {
				pos += 2
			} else {
				pos++
			}

		case 'K':
			add("K")
			if at(1) == 'K' {
				pos += 2
			} else {
				pos++
			}

		case 'L':
			add("L")
			if at(1) == 'L' {
				pos += 2
			} else {
				pos++
			}

		case 'M':
			add("M")
			if at(1) == 'M' {
				pos += 2
			} else {
				pos++
			}

		case 'N':
			add("N")
			if at(1) == 'N' {
				pos += 2
			} else {
				pos++
			}

		case 'P':
			if at(1) == 'H' {
				add("F")
				pos += 2
			} else {
				add("P")
				if at(1) == 'P' {
					pos += 2
				} else {
					pos++
				}
			}

		case 'Q':
			add("K")
			if at(1) == 'Q' {
				pos += 2
			} else {
				pos++
			}

		case 'R':
			add("R")
			if at(1) == 'R' {
				pos += 2
			} else {
				pos++
			}

		case 'S':
			if at(1) == 'H' {
				add("X")
				pos += 2
			} else if at(1) == 'C' {
				if at(2) == 'H' {
					add("SK")
					pos += 3
				} else if at(2) == 'I' || at(2) == 'E' || at(2) == 'Y' {
					add("S")
					pos += 3
				} else {
					add("SK")
					pos += 2
				}
			} else {
				add("S")
				if at(1) == 'S' || at(1) == 'Z' {
					pos += 2
				} else {
					pos++
				}
			}

		case 'T':
			if at(1) == 'H' {
				add("0")
				pos += 2
			} else if at(1) == 'I' && (at(2) == 'O' || at(2) == 'A') {
				add("X")
				pos += 3
			} else {
				add("T")
				if at(1) == 'T' {
					pos += 2
				} else {
					pos++
				}
			}

		case 'V':
			add("F")
			if at(1) == 'V' {
				pos += 2
			} else {
				pos++
			}

		case 'W':
			if isVowel(at(1)) {
				add("A")
				pos += 2
			} else {
				pos++
			}

		case 'X':
			add("KS")
			if at(1) == 'X' {
				pos += 2
			} else {
				pos++
			}

		case 'Z':
			add("S")
			if at(1) == 'Z' {
				pos += 2
			} else {
				pos++
			}

		default:
			pos++
		}
	}

	return pri.String(), alt.String()
}

// MetaphoneKey returns the primary metaphone code for a normalized term,
// splitting multi-word terms and concatenating codes.
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
