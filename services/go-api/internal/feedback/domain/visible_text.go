package domain

import (
	"strings"
	"unicode"
)

func visibleRuneCount(message string) int {
	count := 0
	for _, r := range strings.TrimFunc(message, isBlank) {
		if !isInvisible(r) {
			count++
		}
	}
	return count
}

func isBlank(r rune) bool { return unicode.IsSpace(r) || isInvisible(r) }

var invisibleRanges = []*unicode.RangeTable{
	unicode.Cf,
	unicode.Other_Default_Ignorable_Code_Point,
	unicode.Variation_Selector,
}

const brailleBlank = '\u2800'

func isInvisible(r rune) bool {
	return unicode.In(r, invisibleRanges...) || r == brailleBlank || (unicode.IsControl(r) && !unicode.IsSpace(r))
}

func visibleText(s string) string {
	s = strings.Map(func(r rune) rune {
		if isInvisible(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
