package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
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
	titleTokens := music.TitleTokens(trackTitle)
	artistTokens := music.TitleTokens(trackArtist)
	for _, entry := range music.MatchVersionPhrases(music.TitleTokens(music.StripFeatureCredits(candidateTitle))) {
		if entry.Benign() || entry.In(titleTokens) || entry.In(artistTokens) {
			continue
		}
		if entry.Veto {
			veto = music.AppendVersionLabel(veto, entry.Label)
			continue
		}
		qualifiers = music.AppendVersionLabel(qualifiers, entry.Label)
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
	labels := music.RecordingLabels(title)
	tokens := music.TitleTokens(music.StripFeatureCredits(title))
	for rest := tokens; len(rest) > 0; {
		marker := presentationMarkerAt(rest)
		if marker == nil {
			rest = rest[1:]
			continue
		}
		labels = music.AppendVersionLabel(labels, strings.Join(marker, " "))
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
	if _, wanted := music.StripRerecordingPhrases(music.TitleTokens(wantedTitle)); wanted {
		return false
	}
	_, carried := music.StripRerecordingPhrases(music.TitleTokens(candidateTitle))
	return carried
}
