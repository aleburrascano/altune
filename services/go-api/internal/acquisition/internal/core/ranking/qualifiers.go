package ranking

import (
	"altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/shared/versiontag"
	"slices"
	"strings"
)

const (
	unrequestedQualifierCost = 1
	unfulfilledQualifierCost = 2
)

var presentationMarkers = [][]string{
	{"official", "music", "video"},
	{"music", "video"},
	{"official", "video"},
	{"lyric", "video"},
	{"visualizer"},
	{"lyrics"},
	{"video"},
}

func UnrequestedQualifiers(trackTitle, trackArtist, candidateTitle string) (veto, qualifiers []string) {
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
		qualifiers = versiontag.AppendLabel(qualifiers, entry.Label)
	}
	return veto, qualifiers
}

func featureMatch(trackTitle, candidateTitle string) bool {
	candidate := " " + domain.CreditKey(candidateTitle) + " "
	for _, featured := range domain.FeaturedCredits(trackTitle) {
		key := domain.CreditKey(featured)
		if key != "" && !strings.Contains(candidate, " "+key+" ") {
			return false
		}
	}
	return true
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

func distanceLabels(title string) []string {
	labels := versiontag.RecordingLabels(title)
	tokens := versiontag.Tokens(versiontag.WithoutFeatureCredits(title))
	for rest := tokens; len(rest) > 0; {
		marker := presentationMarkerAt(rest)
		if marker == nil {
			rest = rest[1:]
			continue
		}
		labels = versiontag.AppendLabel(labels, strings.Join(marker, " "))
		rest = rest[len(marker):]
	}
	return labels
}

func presentationMarkerAt(tokens []string) []string {
	for _, marker := range presentationMarkers {
		if len(marker) <= len(tokens) && slices.Equal(tokens[:len(marker)], marker) {
			return marker
		}
	}
	return nil
}

func carriesUnrequestedRerecording(wantedTitle, candidateTitle string) bool {
	if _, wanted := versiontag.WithoutRerecordingPhrases(versiontag.Tokens(wantedTitle)); wanted {
		return false
	}
	_, carried := versiontag.WithoutRerecordingPhrases(versiontag.Tokens(candidateTitle))
	return carried
}
