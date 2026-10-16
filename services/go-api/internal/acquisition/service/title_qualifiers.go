package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strings"
)

var featuredRe = versiontag.FeaturedCredit

func featureMatch(trackTitle, candidateTitle string) bool {
	cand := " " + acqdomain.CreditKey(candidateTitle) + " "
	for _, f := range acqdomain.FeaturedCredits(trackTitle) {
		key := acqdomain.CreditKey(f)
		if key != "" && !strings.Contains(cand, " "+key+" ") {
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
