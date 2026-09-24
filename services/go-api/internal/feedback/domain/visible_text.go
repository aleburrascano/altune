package domain

import (
	"strings"
	"unicode"
)

func visibleClusterCount(message string) int {
	runes := []rune(strings.TrimFunc(message, isBlank))
	count := 0
	for i, r := range runes {
		if !isInvisible(r) && (i == 0 || isClusterBoundary(runes, i)) {
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
