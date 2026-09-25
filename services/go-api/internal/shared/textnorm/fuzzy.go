package textnorm

import (
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFuzzyRunes = 512

func capRunes(r []rune) []rune {
	if len(r) > maxFuzzyRunes {
		return r[:maxFuzzyRunes]
	}
	return r
}

func TokenSortRatio(s1, s2 string) float64 {
	t1 := sortedTokenString(s1)
	t2 := sortedTokenString(s2)
	return levenshteinRatio(t1, t2) * 100
}

func sortedTokenString(s string) string {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

func levenshteinRatio(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	total := min(utf8.RuneCountInString(truncateBytes(s1)), maxFuzzyRunes) + min(utf8.RuneCountInString(truncateBytes(s2)), maxFuzzyRunes)
	if total == 0 {
		return 1.0
	}
	dist := LevenshteinDistance(s1, s2)
	matching := total - 2*dist
	if matching < 0 {
		matching = 0
	}
	return float64(matching) / float64(total)
}

func truncateBytes(s string) string {
	if len(s) > maxFuzzyRunes*utf8.UTFMax {
		return s[:maxFuzzyRunes*utf8.UTFMax]
	}
	return s
}

func LevenshteinDistance(s1, s2 string) int {
	r1 := capRunes([]rune(truncateBytes(s1)))
	r2 := capRunes([]rune(truncateBytes(s2)))
	if len(r1) == 0 {
		return len(r2)
	}
	if len(r2) == 0 {
		return len(r1)
	}
	prev := make([]int, len(r2)+1)
	curr := make([]int, len(r2)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(r1); i++ {
		curr[0] = i
		for j := 1; j <= len(r2); j++ {
			cost := 1
			if r1[i-1] == r2[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			curr[j] = best
		}
		prev, curr = curr, prev
	}
	return prev[len(r2)]
}
