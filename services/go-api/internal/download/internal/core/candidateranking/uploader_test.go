package candidateranking_test

import (
	"altune/go-api/internal/download"
	"testing"
)

func TestRank_ArtistMatchesAChannelByItsWholeName(t *testing.T) {
	tests := []struct {
		artist  string
		channel string
		want    bool
	}{
		{"The Weeknd", "The Weeknd - Topic", true},
		{"Kendrick Lamar", "Kendrick Lamar - Topic", true},
		{"Dr. Dre", "Kendrick Lamar - Topic", false},
		{"Post Malone", "Post Malone - Topic", true},
		{"Post Malone", "Mac Miller - Topic", false},
		{"The Weeknd", "TheWeekndVEVO", true},
		{"Eve", "Eve Official", true},
		{"Bad Bunny", "RandomUploader", false},
		{"!!!", "Random - Topic", false},
		{"!!!", "", false},
		{"???", "TheWeekndVEVO", false},
		{"", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.artist+"_"+tt.channel, func(t *testing.T) {
			c := download.AudioCandidate{Title: "Song", Channel: tt.channel, Kind: download.KindUpload}
			if got := evaluation(t, song("Song", tt.artist, 0), c)["artist_match"]; got != tt.want {
				t.Errorf("artist_match(%q, %q) = %v, want %v", tt.artist, tt.channel, got, tt.want)
			}
		})
	}
}

func TestRank_ACreditedKindMatchesTheArtistByItsCredits(t *testing.T) {
	tests := []struct {
		name string
		kind download.CandidateKind
		want bool
	}{
		{"credited art track", download.KindArtTrack, true},
		{"credited pinned video", download.KindPinnedVideo, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credited := download.AudioCandidate{Title: "Song", Channel: "Somewhere", Kind: tt.kind, Artists: []string{"Artist"}}
			if got := evaluation(t, song("Song", "Artist", 0), credited)["artist_match"]; got != tt.want {
				t.Errorf("artist_match = %v, want %v", got, tt.want)
			}
			uncredited := download.AudioCandidate{Title: "Song", Channel: "Artist", Kind: tt.kind, Artists: []string{"Someone Else"}}
			if got := evaluation(t, song("Song", "Artist", 0), uncredited)["artist_match"]; got != false {
				t.Errorf("artist_match for an uncredited %s on the artist's channel = %v, want false", tt.kind, got)
			}
		})
	}
}

func TestRank_IsTopicFollowsTheUploaderClass(t *testing.T) {
	tests := []struct {
		channel string
		want    bool
	}{
		{"Artist - Topic", true},
		{"ArtistVEVO", false},
		{"Topic - Artist", false},
		{"", false},
	}
	for _, tt := range tests {
		c := download.AudioCandidate{Title: "Song", Channel: tt.channel}
		if got := evaluation(t, song("Song", "Artist", 0), c)["is_topic"]; got != tt.want {
			t.Errorf("is_topic for %q = %v, want %v", tt.channel, got, tt.want)
		}
	}
}
