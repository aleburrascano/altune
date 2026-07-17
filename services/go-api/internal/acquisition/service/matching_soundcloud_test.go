package service

import (
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

// These characterization tests lock the behaviour the acquire-soundcloud spec
// relies on: a SoundCloud upload (a non-Topic channel) can WIN selection when it
// is the only identity-passing candidate, but can NEVER displace a qualifying
// YouTube "- Topic" candidate. The production selection code is unchanged — these
// guard the SoundCloud acquisition path against a future selection refactor.

func TestSelectBestCandidate_SoundCloudFillsGap(t *testing.T) {
	track := TrackRef{Title: "Fell In Love", Artist: "Lil Tecca", Duration: 150}

	// Only candidate: a SoundCloud upload (non-Topic channel) with a strong title
	// match and matching duration. No qualifying YouTube candidate exists — this
	// is the underground long tail YouTube does not index.
	candidates := []ports.AudioCandidate{
		{
			Title:    "Lil Tecca - Fell In Love",
			Channel:  "Lil Tecca",
			Duration: 150,
			URL:      "https://soundcloud.com/liltecca/fell-in-love",
		},
	}

	got := SelectBestCandidate(track, candidates)
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
		// SoundCloud re-upload (non-Topic).
		{
			Title:    "The Weeknd - Blinding Lights",
			Channel:  "The Weeknd",
			Duration: 200,
			URL:      "https://soundcloud.com/x/blinding-lights",
		},
		// Official YouTube auto-generated Topic channel.
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			Categories: []string{"Music"},
			URL:        "https://youtube.com/watch?v=topic",
		},
	}

	got := SelectBestCandidate(track, candidates)
	if got == nil {
		t.Fatal("expected a candidate to be selected, got nil")
	}
	if got.URL != "https://youtube.com/watch?v=topic" {
		t.Fatalf("Topic channel must win over SoundCloud; got %+v", got)
	}
}
