package service

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	durationTolerancePct = 0.15
	minMatchScore        = 60.0
)

func SelectBestCandidate(track TrackRef, candidates []Candidate) *Candidate {
	if len(candidates) == 0 {
		return nil
	}

	var scored []Candidate
	for _, c := range candidates {
		if track.Duration > 0 && c.Duration > 0 {
			ratio := math.Abs(track.Duration-c.Duration) / track.Duration
			if ratio > durationTolerancePct {
				continue
			}
		}

		identity := combineIdentity(track.Title, track.Artist)
		candidateIdentity := combineIdentity(c.Title, c.Artist)
		score := TokenSortRatio(identity, candidateIdentity)

		if score < minMatchScore {
			continue
		}

		c.Score = score
		scored = append(scored, c)
	}

	if len(scored) == 0 {
		return nil
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return isMusicChannel(scored[i].Categories) && !isMusicChannel(scored[j].Categories)
	})

	result := scored[0]
	return &result
}

func combineIdentity(title, artist string) string {
	return normalizeForMatch(title) + " " + normalizeForMatch(artist)
}

func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isMusicChannel(categories []string) bool {
	for _, c := range categories {
		if strings.EqualFold(c, "Music") {
			return true
		}
	}
	return false
}

// TokenSortRatio implements rapidfuzz's token_sort_ratio algorithm:
// tokenize → sort tokens alphabetically → join → compute normalized
// Levenshtein ratio * 100.
func TokenSortRatio(s1, s2 string) float64 {
	t1 := sortedTokens(s1)
	t2 := sortedTokens(s2)
	return levenshteinRatio(t1, t2) * 100
}

func sortedTokens(s string) string {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

func levenshteinRatio(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	lenS1 := len(s1)
	lenS2 := len(s2)
	total := lenS1 + lenS2
	if total == 0 {
		return 1.0
	}

	dist := levenshteinDistance(s1, s2)
	matching := total - 2*dist
	if matching < 0 {
		matching = 0
	}
	return float64(matching) / float64(total)
}

func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	prev := make([]int, len(s2)+1)
	curr := make([]int, len(s2)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(s1); i++ {
		curr[0] = i
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(s2)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
