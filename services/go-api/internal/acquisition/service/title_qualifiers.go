package service

import (
	"altune/go-api/internal/acquisition/service/versiontag"
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"strings"
)

var featuredRe = versiontag.FeaturedCredit

var featSepRe = regexp.MustCompile(`(?i)\s*(?:,|&|\band\b)\s*`)

func extractFeaturedArtists(title string) []string {
	m := featuredRe.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	parts := featSepRe.Split(strings.TrimSpace(m[1]), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func featureMatch(trackTitle, candidateTitle string) bool {
	feats := extractFeaturedArtists(trackTitle)
	if len(feats) == 0 {
		return true
	}
	cand := strings.ToLower(candidateTitle)
	for _, f := range feats {
		if !strings.Contains(cand, strings.ToLower(f)) {
			return false
		}
	}
	return true
}

var qualifierRe = regexp.MustCompile(`[\(\[\{]([^\)\]\}]*)[\)\]\}]`)

func qualifierTokens(title string) map[string]bool {
	tokens := make(map[string]bool)
	for _, segment := range qualifierRe.FindAllStringSubmatch(textnorm.NFKC(title), -1) {
		if featuredRe.MatchString(segment[1]) {
			continue
		}
		for _, word := range strings.Fields(textnorm.NormalizeForMatch(segment[1])) {
			tokens[word] = true
		}
	}
	return tokens
}

func qualifierDistance(trackTitle, candidateTitle string) int {
	want := qualifierTokens(trackTitle)
	got := qualifierTokens(candidateTitle)

	distance := 0
	for token := range got {
		if !want[token] {
			distance += unrequestedQualifierCost
		}
	}
	for token := range want {
		if !got[token] {
			distance += unfulfilledQualifierCost
		}
	}
	return distance
}

func UnrequestedQualifiers(trackTitle, trackArtist, candidateTitle string) (veto, fallback []string) {
	titleTokens := versiontag.Tokens(trackTitle)
	artistTokens := versiontag.Tokens(trackArtist)
	for _, entry := range versiontag.Matches(versiontag.Tokens(versiontag.WithoutFeatureCredits(candidateTitle))) {
		if entry.Benign() || entry.In(titleTokens) || entry.In(artistTokens) {
			continue
		}
		if entry.Veto {
			veto = versiontag.AppendLabel(veto, entry.Label)
			continue
		}
		fallback = versiontag.AppendLabel(fallback, entry.Label)
	}
	return veto, fallback
}
