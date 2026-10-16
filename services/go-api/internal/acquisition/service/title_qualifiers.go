package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared/versiontag"
	"slices"
	"strings"
)

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

var presentationMarkers = [][]string{
	{"official", "music", "video"},
	{"music", "video"},
	{"official", "video"},
	{"lyric", "video"},
	{"visualizer"},
	{"lyrics"},
	{"video"},
}

func distanceLabels(title string) []string {
	labels := versiontag.RecordingLabels(title)
	tokens := versiontag.Tokens(versiontag.WithoutFeatureCredits(title))
	for pos := 0; pos < len(tokens); {
		advanced := false
		for _, marker := range presentationMarkers {
			if pos+len(marker) <= len(tokens) && slices.Equal(tokens[pos:pos+len(marker)], marker) {
				labels = versiontag.AppendLabel(labels, strings.Join(marker, " "))
				pos += len(marker)
				advanced = true
				break
			}
		}
		if !advanced {
			pos++
		}
	}
	return labels
}

func qualifierDistance(trackTitle, candidateTitle string) int {
	want := distanceLabels(trackTitle)
	got := distanceLabels(candidateTitle)

	distance := 0
	for _, label := range got {
		if !slices.Contains(want, label) {
			distance += unrequestedQualifierCost
		}
	}
	for _, label := range want {
		if !slices.Contains(got, label) {
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
