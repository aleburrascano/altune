package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"regexp"
	"slices"
	"strings"
)

const (
	lengthToleranceSeconds = 3.0
	rankMargin             = 0.04
	idWeight               = 5.0
	artistWeight           = 3.0
	titleWeight            = 3.0
	lengthWeight           = 2.0
	percent                = 100.0
)

type guard string

const (
	guardISRC   guard = "isrc"
	guardArtist guard = "artist"
	guardTitle  guard = "title"
	guardLength guard = "length"
)

var artistSeparatorRe = regexp.MustCompile(`(?i)\s*(?:,|;|&|\band\b|\bfeat\.?(?:\s|$)|\bft\.?(?:\s|$)|\bfeaturing\b|\bwith\b|\bx\b)\s*`)

var versionSuffixRe = regexp.MustCompile(`(?i)^(.*\S)\s[-–—]\s+([^-–—|]*\b(?:original|album|single|mono|stereo|deluxe|bonus|version|edition)\b[^-–—|]*)$`)

type judged struct {
	candidate
	Failures []guard `json:"failures,omitempty"`
	Distance float64 `json:"distance"`
}

func (j judged) isGuarded() bool { return len(j.Failures) == 0 }

func judge(t libraryTrack, c candidate) judged {
	return judged{candidate: c, Failures: failedGuards(t, c), Distance: distance(t, c)}
}

func failedGuards(t libraryTrack, c candidate) []guard {
	var failed []guard
	if !artistMatches(t.Artist, c.Artists) {
		failed = append(failed, guardArtist)
	}
	if titleKey(t.Title) != titleKey(c.Title) {
		failed = append(failed, guardTitle)
	}
	if math.Abs(t.FileSeconds-c.LengthSeconds) > lengthToleranceSeconds {
		failed = append(failed, guardLength)
	}
	if canonicalISRC(t.ISRC) != canonicalISRC(c.ISRC) {
		failed = append(failed, guardISRC)
	}
	return failed
}

func bracketVersionSuffix(title string) string {
	m := versionSuffixRe.FindStringSubmatch(title)
	if m == nil {
		return title
	}
	return m[1] + " (" + m[2] + ")"
}

func titleKey(title string) string {
	keys := domain.CleanSearchKeys(bracketVersionSuffix(title), "", "")
	return strings.Join(append([]string{textnorm.NormalizeForMatch(keys.Title)}, keys.VariantQualifiers()...), textnorm.KeySeparator)
}

func artistKey(name string) string {
	if key := textnorm.NormalizeForMatch(name); key != "" {
		return key
	}
	return textnorm.FoldName(name)
}

func artistKeys(name string) []string {
	keys := []string{artistKey(name)}
	for _, part := range artistSeparatorRe.Split(name, -1) {
		keys = append(keys, artistKey(part))
	}
	return slices.DeleteFunc(keys, func(key string) bool { return key == "" })
}

func artistMatches(stored string, providerNames []string) bool {
	storedKeys := artistKeys(stored)
	for _, name := range providerNames {
		for _, key := range artistKeys(name) {
			if slices.Contains(storedKeys, key) {
				return true
			}
		}
	}
	return false
}

func distance(t libraryTrack, c candidate) float64 {
	idGap := 0.0
	if canonicalISRC(t.ISRC) != canonicalISRC(c.ISRC) {
		idGap = 1
	}
	lengthGap := min(math.Abs(t.FileSeconds-c.LengthSeconds)/lengthToleranceSeconds, 1)
	total := idWeight*idGap + artistWeight*artistGap(t.Artist, c.Artists) + titleWeight*titleGap(t.Title, c.Title) + lengthWeight*lengthGap
	return total / (idWeight + artistWeight + titleWeight + lengthWeight)
}

func titleGap(stored, provider string) float64 {
	return 1 - textnorm.TokenSortRatio(textnorm.NormalizeForMatch(stored), textnorm.NormalizeForMatch(provider))/percent
}

func artistGap(stored string, providerNames []string) float64 {
	gap := 1.0
	for _, name := range providerNames {
		gap = min(gap, 1-textnorm.TokenSortRatio(textnorm.NormalizeForMatch(stored), textnorm.NormalizeForMatch(name))/percent)
	}
	return gap
}
