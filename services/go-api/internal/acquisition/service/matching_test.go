package service

import (
	"altune/go-api/internal/acquisition/ports"

	"testing"
)

func TestSelectBestCandidate(t *testing.T) {
	tests := []struct {
		name       string
		track      TrackRef
		candidates []ports.AudioCandidate
		wantNil    bool
		wantTitle  string
		wantReason string // human-readable why this candidate wins
	}{
		{
			name: "topic channel preferred when identity >= 60",
			track: TrackRef{
				Title:    "Blinding Lights",
				Artist:   "The Weeknd",
				Duration: 200,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "The Weeknd - Blinding Lights",
					Channel:    "TheWeekndVEVO",
					Duration:   203,
					URL:        "https://youtube.com/watch?v=vevo1",
					Categories: []string{"Music"},
					ViewCount:  500_000_000,
				},
				{
					Title:      "Blinding Lights",
					Channel:    "The Weeknd - Topic",
					Duration:   200,
					URL:        "https://youtube.com/watch?v=topic1",
					Categories: []string{"Music"},
					ViewCount:  10_000_000,
				},
			},
			wantTitle:  "Blinding Lights",
			wantReason: "topic channel candidate should be preferred over VEVO when identity >= 60",
		},
		{
			name: "exact duration match scores highest among non-topic candidates",
			track: TrackRef{
				Title:    "Starboy",
				Artist:   "The Weeknd",
				Duration: 230,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "The Weeknd - Starboy (Extended Remix)",
					Channel:    "RandomUploader",
					Duration:   300,
					URL:        "https://youtube.com/watch?v=far",
					Categories: []string{"Music"},
					ViewCount:  1_000,
				},
				{
					Title:      "The Weeknd - Starboy (Audio)",
					Channel:    "AnotherUploader",
					Duration:   231,
					URL:        "https://youtube.com/watch?v=close",
					Categories: []string{"Music"},
					ViewCount:  1_000,
				},
			},
			wantTitle:  "The Weeknd - Starboy (Audio)",
			wantReason: "candidate with duration within durationTight (3s) should score higher than one 70s off",
		},
		{
			name: "no candidates returns nil",
			track: TrackRef{
				Title:  "Nonexistent",
				Artist: "Nobody",
			},
			candidates: []ports.AudioCandidate{},
			wantNil:    true,
			wantReason: "empty candidate list must return nil",
		},
		{
			name: "all candidates below identity threshold filtered out",
			track: TrackRef{
				Title:    "Save Your Tears",
				Artist:   "The Weeknd",
				Duration: 215,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "Cooking Tutorial Episode 47",
					Channel:    "CookingChannel",
					Duration:   215,
					URL:        "https://youtube.com/watch?v=cook1",
					Categories: []string{"Howto & Style"},
					ViewCount:  50_000,
				},
				{
					Title:      "Random Podcast About Finance",
					Channel:    "FinanceBro",
					Duration:   3600,
					URL:        "https://youtube.com/watch?v=fin1",
					Categories: []string{"Education"},
					ViewCount:  10_000,
				},
			},
			wantNil:    true,
			wantReason: "candidates with titles completely unrelated to track should score identity < 60 and be filtered out",
		},
		{
			name: "highest composite score wins among non-topic candidates",
			track: TrackRef{
				Title:    "Die For You",
				Artist:   "The Weeknd",
				Duration: 260,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "The Weeknd - Die For You (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   262,
					URL:        "https://youtube.com/watch?v=vevo2",
					Categories: []string{"Music"},
					ViewCount:  900_000_000,
				},
				{
					Title:      "The Weeknd - Die For You (Lyrics)",
					Channel:    "LyricsChannel",
					Duration:   261,
					URL:        "https://youtube.com/watch?v=lyrics1",
					Categories: []string{"Music"},
					ViewCount:  50_000_000,
				},
			},
			wantTitle:  "The Weeknd - Die For You (Official Video)",
			wantReason: "VEVO channel (0.8) + highest views + Music category should beat a lyrics channel (0.3)",
		},
		{
			name: "nil candidates returns nil",
			track: TrackRef{
				Title:  "Test",
				Artist: "Test",
			},
			candidates: nil,
			wantNil:    true,
			wantReason: "nil candidate slice must return nil",
		},
		{
			name: "topic channel wins over VEVO even with fewer views",
			track: TrackRef{
				Title:    "After Hours",
				Artist:   "The Weeknd",
				Duration: 361,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "The Weeknd - After Hours (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   362,
					URL:        "https://youtube.com/watch?v=vevo3",
					Categories: []string{"Music"},
					ViewCount:  800_000_000,
				},
				{
					Title:      "After Hours",
					Channel:    "The Weeknd - Topic",
					Duration:   361,
					URL:        "https://youtube.com/watch?v=topic2",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantTitle:  "After Hours",
			wantReason: "topic channel is unconditionally preferred over non-topic when identity passes threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectBestCandidate(tt.track, tt.candidates)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got candidate with title %q — %s", got.Title, tt.wantReason)
				}
				return
			}

			if got == nil {
				t.Fatalf("expected candidate with title %q, got nil — %s", tt.wantTitle, tt.wantReason)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("got title %q, want %q — %s", got.Title, tt.wantTitle, tt.wantReason)
			}
		})
	}
}

func TestDurationScore(t *testing.T) {
	tests := []struct {
		name     string
		expected float64
		actual   float64
		want     float64
	}{
		{name: "exact match", expected: 200, actual: 200, want: 1.0},
		{name: "within tight threshold", expected: 200, actual: 202, want: 1.0},
		{name: "at tight boundary", expected: 200, actual: 203, want: 1.0},
		{name: "between tight and loose", expected: 200, actual: 210, want: 0.5},
		{name: "at loose boundary", expected: 200, actual: 215, want: 0.5},
		{name: "beyond loose threshold", expected: 200, actual: 216, want: 0.0},
		{name: "zero expected returns 0.5", expected: 0, actual: 200, want: 0.5},
		{name: "zero actual returns 0.5", expected: 200, actual: 0, want: 0.5},
		{name: "both zero returns 0.5", expected: 0, actual: 0, want: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := durationScore(tt.expected, tt.actual)
			if got != tt.want {
				t.Errorf("durationScore(%v, %v) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

func TestChannelScore(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		want    float64
	}{
		{name: "topic channel", channel: "The Weeknd - Topic", want: 1.0},
		{name: "vevo channel", channel: "TheWeekndVEVO", want: 0.8},
		{name: "vevo mixed case", channel: "SomeArtistVevo", want: 0.8},
		{name: "regular channel", channel: "RandomUploader", want: 0.3},
		{name: "empty channel", channel: "", want: 0.3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := channelScore(tt.channel)
			if got != tt.want {
				t.Errorf("channelScore(%q) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}

func TestCategoryScore(t *testing.T) {
	tests := []struct {
		name       string
		categories []string
		want       float64
	}{
		{name: "music category present", categories: []string{"Music"}, want: 1.0},
		{name: "music among others", categories: []string{"Entertainment", "Music"}, want: 1.0},
		{name: "no music category", categories: []string{"Education", "Howto & Style"}, want: 0.2},
		{name: "empty categories", categories: []string{}, want: 0.2},
		{name: "nil categories", categories: nil, want: 0.2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categoryScore(tt.categories)
			if got != tt.want {
				t.Errorf("categoryScore(%v) = %v, want %v", tt.categories, got, tt.want)
			}
		})
	}
}

func TestViewScore(t *testing.T) {
	tests := []struct {
		name      string
		viewCount int64
		maxViews  int64
		want      float64
	}{
		{name: "max views", viewCount: 1000, maxViews: 1000, want: 1.0},
		{name: "half views", viewCount: 500, maxViews: 1000, want: 0.5},
		{name: "zero max returns 0.5", viewCount: 100, maxViews: 0, want: 0.5},
		{name: "zero views zero max", viewCount: 0, maxViews: 0, want: 0.5},
		{name: "exceeds max capped at 1.0", viewCount: 2000, maxViews: 1000, want: 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := viewScore(tt.viewCount, tt.maxViews)
			if got != tt.want {
				t.Errorf("viewScore(%d, %d) = %v, want %v", tt.viewCount, tt.maxViews, got, tt.want)
			}
		})
	}
}

func TestIsTopicChannel(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		want    bool
	}{
		{name: "is topic", channel: "Artist - Topic", want: true},
		{name: "not topic", channel: "ArtistVEVO", want: false},
		{name: "partial match", channel: "Topic - Artist", want: false},
		{name: "empty", channel: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTopicChannel(tt.channel)
			if got != tt.want {
				t.Errorf("isTopicChannel(%q) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}
