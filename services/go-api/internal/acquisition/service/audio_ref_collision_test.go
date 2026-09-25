package service

import "testing"

func collisionRef(title string) string {
	return BuildAudioRef(TrackRef{UserID: "u", Artist: "A", Album: "B", Title: title}, "x.mp3")
}

func TestBuildAudioRef_DistinctRecordingsNeverShareAKey(t *testing.T) {
	distinct := [][2]string{
		{"Song", "Song (Live)"},
		{"Song (Live)", "Song (Demo)"},
		{"!!!", "???"},
		{"(Untitled)", "!!!"},
	}
	for _, pair := range distinct {
		if collisionRef(pair[0]) == collisionRef(pair[1]) {
			t.Errorf("%q and %q share key %q", pair[0], pair[1], collisionRef(pair[0]))
		}
	}
}

func TestBuildAudioRef_VariantsOfOneRecordingShareAKey(t *testing.T) {
	if collisionRef("Café Song") != collisionRef("CAFE song") {
		t.Errorf("case/diacritic variants diverged: %q vs %q", collisionRef("Café Song"), collisionRef("CAFE song"))
	}
}
