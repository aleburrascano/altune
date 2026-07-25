package service

import (
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

func TestSelectBestCandidate_SoundCloudFillsGap(t *testing.T) {
	track := TrackRef{Title: "Fell In Love", Artist: "Lil Tecca", Duration: 150}

	candidates := []ports.AudioCandidate{
		{
			Title:    "Lil Tecca - Fell In Love",
			Channel:  "Lil Tecca",
			Duration: 150,
			URL:      "https://soundcloud.com/liltecca/fell-in-love",
		},
	}

	got := selectBest(track, candidates)
	if got == nil {
		t.Fatal("expected the SoundCloud candidate to be selected, got nil")
	}
	if got.URL != "https://soundcloud.com/liltecca/fell-in-love" {
		t.Fatalf("selected wrong candidate: %+v", got)
	}
}

func TestSelectBestCandidate_TopicChannelBeatsSoundCloud(t *testing.T) {
	track := TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200}

	candidates := []ports.AudioCandidate{
		{
			Title:    "The Weeknd - Blinding Lights",
			Channel:  "The Weeknd",
			Duration: 200,
			URL:      "https://soundcloud.com/x/blinding-lights",
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			Categories: []string{"Music"},
			URL:        "https://youtube.com/watch?v=topic",
		},
	}

	got := selectBest(track, candidates)
	if got == nil {
		t.Fatal("expected a candidate to be selected, got nil")
	}
	if got.URL != "https://youtube.com/watch?v=topic" {
		t.Fatalf("Topic channel must win over SoundCloud; got %+v", got)
	}
}
