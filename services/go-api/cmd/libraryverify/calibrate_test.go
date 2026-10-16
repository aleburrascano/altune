package main

import (
	"altune/go-api/internal/acquisition/adapters/deezerpreview"
	"bytes"
	"strings"
	"testing"
)

func printedSong(id, artist, title, isrc string, seed uint64) printedTrack {
	audio := randomPrint(seed, 1400)
	return printedTrack{
		track: libraryTrack{ID: id, Artist: artist, Title: title, Identity: boundIdentity{ISRC: isrc}},
		file:  audio,
		clip:  deezerpreview.Clip{Print: audio[300:540]},
	}
}

func TestScorePairs_ScoresEachSongAgainstItsOwnPreviewAndAnotherByTheSameArtist(t *testing.T) {
	printed := []printedTrack{
		printedSong("t-1", "Artist", "One", "ISRC1", 1),
		printedSong("t-2", "Artist", "Two", "ISRC2", 2),
		printedSong("t-3", "Solo", "Only", "ISRC3", 3),
	}

	same, different := scorePairs(printed)

	if len(same) != 3 {
		t.Fatalf("same-song pairs = %d, want one per track", len(same))
	}
	for _, pair := range same {
		if pair.BitErrorRate != 0 {
			t.Errorf("same-song pair %+v, want a zero bit error rate", pair)
		}
	}
	if len(different) != 2 {
		t.Fatalf("different-song pairs = %d, want the two tracks by Artist against each other and none for Solo", len(different))
	}
	for _, pair := range different {
		if pair.TrackID == pair.OtherTrackID || pair.BitErrorRate < 0.4 || pair.Artist != "Artist" {
			t.Errorf("different-song pair %+v, want two distinct songs by Artist scoring near 0.5", pair)
		}
	}
}

func TestScorePairs_AnotherReleaseOfTheSameSongIsNotADifferentSong(t *testing.T) {
	first := printedSong("t-1", "Artist", "One", "ISRC1", 1)
	tests := []struct {
		name  string
		other printedTrack
	}{
		{"same title once brackets are dropped", printedSong("t-2", "Artist", "One (Remastered)", "ISRC2", 2)},
		{"same ISRC under another title", printedSong("t-3", "Artist", "Another Name", "ISRC1", 3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, different := scorePairs([]printedTrack{first, tt.other}); len(different) != 0 {
				t.Errorf("different-song pairs = %+v, want none", different)
			}
		})
	}
}

func TestSummarize_ReportsTheSpreadOfTheScores(t *testing.T) {
	var pairs []pairScore
	for _, rate := range []float64{0.5, 0.1, 0.3, 0.2, 0.4} {
		pairs = append(pairs, pairScore{BitErrorRate: rate})
	}

	got := summarize(pairs)

	want := distribution{Count: 5, Min: 0.1, P05: 0.1, Median: 0.3, P95: 0.5, Max: 0.5}
	if got != want {
		t.Errorf("distribution = %+v, want %+v", got, want)
	}
	if empty := summarize(nil); empty != (distribution{}) {
		t.Errorf("empty distribution = %+v, want zero", empty)
	}
}

func TestWriteCalibrationSummary_PrintsBothDistributions(t *testing.T) {
	var out bytes.Buffer
	report := calibrationReport{Same: distribution{Count: 3, Max: 0.12}, Different: distribution{Count: 2, Min: 0.44}, Skipped: []string{"x"}}

	writeCalibrationSummary(&out, report, "/x/cal.json")

	for _, want := range []string{"same song", "n=3", "max=0.120", "different song", "min=0.440", "skipped: 1", "/x/cal.json"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, out.String())
		}
	}
}
