package ranking_test

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

const (
	unrequestedMarkerCost = 1
	unfulfilledMarkerCost = 2
)

func qualifierDistance(t *testing.T, trackTitle, candidateTitle string) int {
	t.Helper()
	return int(number(t, evaluation(t, song(trackTitle, "Someone", 0), acquisition.AudioCandidate{Title: candidateTitle}), "qualifier_distance"))
}

func featureMatch(t *testing.T, trackTitle, candidateTitle string) any {
	t.Helper()
	return evaluation(t, song(trackTitle, "Someone", 0), acquisition.AudioCandidate{Title: candidateTitle})["feature_match"]
}

func TestQualifierDistance_UnrequestedMarkersCost(t *testing.T) {
	if got := qualifierDistance(t, "Sunglasses at Night", "Sunglasses at Night"); got != 0 {
		t.Errorf("identical bare titles = %d, want 0", got)
	}
	if got := qualifierDistance(t, "Sunglasses at Night", "Sunglasses at Night (Acoustic Version)"); got == 0 {
		t.Error("an unrequested (Acoustic Version) must cost something")
	}
}

func TestQualifierDistance_IsAsymmetric(t *testing.T) {
	if got := qualifierDistance(t, "Song", "Song (Acoustic)"); got != unrequestedMarkerCost {
		t.Errorf("not asking for an acoustic and getting one = %d, want %d", got, unrequestedMarkerCost)
	}
	if got := qualifierDistance(t, "Song (Acoustic)", "Song"); got != unfulfilledMarkerCost {
		t.Errorf("asking for an acoustic and not getting one = %d, want %d", got, unfulfilledMarkerCost)
	}
}

func TestQualifierDistance_RequestedMarkerIsSatisfied(t *testing.T) {
	if got := qualifierDistance(t, "Song (Acoustic)", "Song (Acoustic)"); got != 0 {
		t.Errorf("a satisfied request = %d, want 0 — a user who saved the acoustic wants the acoustic", got)
	}
}

func TestQualifierDistance_IgnoresFeatureCredits(t *testing.T) {
	if got := qualifierDistance(t, "Song (feat. Carti)", "Song (feat. Carti)"); got != 0 {
		t.Errorf("feature credits are the feature match's job, not the qualifier set's: got %d", got)
	}
	if got := qualifierDistance(t, "Song", "Song (feat. Carti)"); got != 0 {
		t.Errorf("a feature credit must not be counted as a variant marker: got %d", got)
	}
}

func TestQualifierDistance_HandlesFullwidthBrackets(t *testing.T) {
	for _, candidate := range []string{"Song (Live)", "Song （Live）"} {
		if got := qualifierDistance(t, "Song", candidate); got == 0 {
			t.Errorf("qualifier distance of %q = 0, want an unrequested marker cost", candidate)
		}
	}
}

func TestQualifierDistance_CountsEachUnrequestedBracketSegment(t *testing.T) {
	if got := qualifierDistance(t, "Song", "Song (Acoustic) (Live)"); got != 2*unrequestedMarkerCost {
		t.Errorf("two unrequested bracket segments = %d, want %d", got, 2*unrequestedMarkerCost)
	}
}

func TestQualifierDistance_CountsVersionsAndPresentationNotNoise(t *testing.T) {
	for _, candidate := range []string{"Song (2011)", "Song (Official Audio)", "Song (HD)", "Song (2011 Remaster)"} {
		if d := qualifierDistance(t, "Song", candidate); d != 0 {
			t.Errorf("qualifier distance of %q = %d, want 0", candidate, d)
		}
	}
	if d := qualifierDistance(t, "Song", "Song - Live"); d <= 0 {
		t.Errorf("unbracketed live costs %d, want > 0", d)
	}
	if d := qualifierDistance(t, "Video Games", "Video Games"); d != 0 {
		t.Errorf("marker word in the track title costs %d, want 0", d)
	}
}

func TestQualifierDistance_APresentationMarkerCountsOnceWherePhrasesOverlap(t *testing.T) {
	tests := []struct {
		candidate string
		want      int
	}{
		{"Song (Official Music Video)", unrequestedMarkerCost},
		{"Song Official Music Video", unrequestedMarkerCost},
		{"Song (Music Video)", unrequestedMarkerCost},
		{"Song (Lyric Video)", unrequestedMarkerCost},
		{"Song (Official Video) [Lyrics]", 2 * unrequestedMarkerCost},
		{"Song (Visualizer)", unrequestedMarkerCost},
		{"Song Video Music", unrequestedMarkerCost},
	}
	for _, tt := range tests {
		if got := qualifierDistance(t, "Song", tt.candidate); got != tt.want {
			t.Errorf("qualifier distance of %q = %d, want %d", tt.candidate, got, tt.want)
		}
	}
}

func TestFeatureMatch_ComparesCreditKeysAsWholeWords(t *testing.T) {
	cases := []struct {
		name, track, candidate string
		want                   bool
	}{
		{"diacritics, and, hyphen", "Song (feat. Beyoncé & JAY-Z)", "Song (feat. Beyonce and JAY Z)", true},
		{"substring of another name", "Song (feat. Eve)", "Song (feat. Steve Lacy)", false},
		{"missing featured artist", "Song (feat. Eve)", "Song", false},
		{"no featured artist", "Song", "Song (feat. Eve)", true},
		{"a credit with no letters asks for nothing", "Song (feat. !!!)", "Song", true},
	}
	for _, c := range cases {
		if got := featureMatch(t, c.track, c.candidate); got != c.want {
			t.Errorf("%s: feature_match(%q, %q) = %v, want %v", c.name, c.track, c.candidate, got, c.want)
		}
	}
}
