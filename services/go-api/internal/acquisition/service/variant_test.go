package service

import (
	"context"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

func TestQualifierDistance_UnrequestedMarkersCost(t *testing.T) {
	if got := qualifierDistance("Sunglasses at Night", "Sunglasses at Night"); got != 0 {
		t.Errorf("identical bare titles = %d, want 0", got)
	}
	if got := qualifierDistance("Sunglasses at Night", "Sunglasses at Night (Acoustic Version)"); got == 0 {
		t.Error("an unrequested (Acoustic Version) must cost something")
	}
}

func TestQualifierDistance_IsAsymmetric(t *testing.T) {
	unrequested := qualifierDistance("Song", "Song (Acoustic)")
	unfulfilled := qualifierDistance("Song (Acoustic)", "Song")

	if unfulfilled <= unrequested {
		t.Errorf("asking for an acoustic and not getting one (%d) must cost more than not asking and getting one (%d)",
			unfulfilled, unrequested)
	}
}

func TestQualifierDistance_RequestedMarkerIsSatisfied(t *testing.T) {
	if got := qualifierDistance("Song (Acoustic)", "Song (Acoustic)"); got != 0 {
		t.Errorf("a satisfied request = %d, want 0 — a user who saved the acoustic wants the acoustic", got)
	}
}

func TestQualifierDistance_IgnoresFeatureCredits(t *testing.T) {
	if got := qualifierDistance("Song (feat. Carti)", "Song (feat. Carti)"); got != 0 {
		t.Errorf("feature credits are featureMatch's job, not the qualifier set's: got %d", got)
	}
	if got := qualifierDistance("Song", "Song (feat. Carti)"); got != 0 {
		t.Errorf("a feature credit must not be counted as a variant marker: got %d", got)
	}
}

func TestRankCandidates_AcousticLosesToTheMasterOnTheSameTopicChannel(t *testing.T) {
	track := TrackRef{Title: "Sunglasses at Night", Artist: "Corey Hart", Duration: 232}
	candidates := []ports.AudioCandidate{
		{
			Title:      "Sunglasses at Night (Acoustic Version)",
			Channel:    "Corey Hart - Topic",
			Duration:   232,
			URL:        "https://youtube.com/watch?v=acoustic000",
			Categories: []string{"Music"},
			ViewCount:  9_000_000,
		},
		{
			Title:      "Sunglasses at Night",
			Channel:    "Corey Hart - Topic",
			Duration:   232,
			URL:        "https://youtube.com/watch?v=master00000",
			Categories: []string{"Music"},
			ViewCount:  1_000,
		},
	}

	ranked := rankCandidates(context.Background(), track, candidates)
	if len(ranked) != 2 {
		t.Fatalf("ranked = %d, want both", len(ranked))
	}
	if ranked[0].Title != "Sunglasses at Night" {
		t.Fatalf("selected %q — an identical-length acoustic take must lose to the master on qualifier distance",
			ranked[0].Title)
	}
}

func TestRankCandidates_MusicVideoLosesToPlainAudio(t *testing.T) {
	track := TrackRef{Title: "Never Surrender", Artist: "Corey Hart", Duration: 262}
	candidates := []ports.AudioCandidate{
		{
			Title:      "Never Surrender (Official Music Video)",
			Channel:    "Corey Hart - Topic",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=video000000",
			Categories: []string{"Music"},
			ViewCount:  8_000_000,
		},
		{
			Title:      "Never Surrender",
			Channel:    "Corey Hart - Topic",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=audio000000",
			Categories: []string{"Music"},
			ViewCount:  3_000,
		},
	}

	ranked := rankCandidates(context.Background(), track, candidates)
	if ranked[0].Title != "Never Surrender" {
		t.Fatalf("selected %q — the plain audio must outrank the video container", ranked[0].Title)
	}
}

func TestRankCandidates_ProvenanceStillBeatsQualifierDistanceOffTopic(t *testing.T) {
	track := TrackRef{Title: "Die For You", Artist: "The Weeknd", Duration: 260}
	candidates := []ports.AudioCandidate{
		{
			Title:      "The Weeknd - Die For You (Lyrics)",
			Channel:    "LyricsChannel",
			Duration:   261,
			URL:        "https://youtube.com/watch?v=lyrics00000",
			Categories: []string{"Music"},
			ViewCount:  50_000_000,
		},
		{
			Title:      "The Weeknd - Die For You (Official Video)",
			Channel:    "TheWeekndVEVO",
			Duration:   262,
			URL:        "https://youtube.com/watch?v=vevo0000000",
			Categories: []string{"Music"},
			ViewCount:  900_000_000,
		},
	}

	ranked := rankCandidates(context.Background(), track, candidates)
	if ranked[0].Channel != "TheWeekndVEVO" {
		t.Fatalf("selected %q — off Topic, label provenance outranks a shorter qualifier list", ranked[0].Channel)
	}
}
