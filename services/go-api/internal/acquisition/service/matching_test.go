package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"strings"
	"testing"
)

func selectBest(track TrackRef, candidates []ports.AudioCandidate) *ports.AudioCandidate {
	ranked, _ := rankAndCollect(context.Background(), track, candidates)
	if len(ranked) == 0 {
		return nil
	}
	best := ranked[0]
	return &best
}

func TestSelectBestCandidate(t *testing.T) {
	tests := []struct {
		name       string
		track      TrackRef
		candidates []ports.AudioCandidate
		wantNil    bool
		wantTitle  string
		wantReason string
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
			got := selectBest(tt.track, tt.candidates)

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

func TestRankCandidates_TieIsDeterministicAndPrefersExpectedLength(t *testing.T) {
	track := TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200}
	candidates := []ports.AudioCandidate{
		{
			Title:      "Blinding Lights (Slowed + Reverb)",
			Channel:    "The Weeknd - Topic",
			Duration:   240,
			URL:        "https://youtube.com/watch?v=slowed",
			Categories: []string{"Music"},
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			URL:        "https://youtube.com/watch?v=master",
			Categories: []string{"Music"},
		},
	}

	for i := 0; i < 50; i++ {
		ranked, _ := rankAndCollect(context.Background(), track, candidates)
		if len(ranked) != 2 {
			t.Fatalf("expected both candidates ranked, got %d", len(ranked))
		}
		if ranked[0].URL != "https://youtube.com/watch?v=master" {
			t.Fatalf("run %d selected %q; the master must win a full identity tie on expected length",
				i, ranked[0].URL)
		}
	}
}

func TestRankCandidates_TotalTieIsStillDeterministic(t *testing.T) {
	track := TrackRef{Title: "Song", Artist: "Artist"}
	candidates := []ports.AudioCandidate{
		{Title: "Artist - Song", Channel: "Artist - Topic", URL: "https://b.example/x"},
		{Title: "Artist - Song", Channel: "Artist - Topic", URL: "https://a.example/x"},
	}

	first, _ := rankAndCollect(context.Background(), track, candidates)
	if len(first) == 0 {
		t.Fatal("expected candidates to survive the identity gate")
	}
	for i := 0; i < 50; i++ {
		again, _ := rankAndCollect(context.Background(), track, candidates)
		if again[0].URL != first[0].URL {
			t.Fatalf("run %d picked %q, first run picked %q — ranking is not deterministic",
				i, again[0].URL, first[0].URL)
		}
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

func TestAcquisitionMatchingRegression(t *testing.T) {
	tests := []struct {
		name         string
		track        TrackRef
		candidates   []ports.AudioCandidate
		wantNil      bool
		wantChannel  string
		wantContains string
		description  string
	}{
		{
			name: "die hard by intended artist prefers correct Topic channel",
			track: TrackRef{
				Title:    "Die Hard",
				Artist:   "Dr. Dre",
				Duration: 280,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "DIE HARD",
					Channel:    "Kendrick Lamar - Topic",
					Duration:   240,
					URL:        "https://youtube.com/watch?v=kendrick",
					Categories: []string{"Music"},
					ViewCount:  65_000_000,
				},
				{
					Title:      "Die Hard",
					Channel:    "Dr. Dre - Topic",
					Duration:   282,
					URL:        "https://youtube.com/watch?v=dre",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantChannel:  "Dr. Dre - Topic",
			wantContains: "Die Hard",
			description:  "must prefer Topic channel matching track artist over wrong-artist Topic channel",
		},
		{
			name: "die hard kendrick is correct when artist is kendrick",
			track: TrackRef{
				Title:    "DIE HARD",
				Artist:   "Kendrick Lamar",
				Duration: 238,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "Kendrick Lamar - DIE HARD (Official Audio)",
					Channel:    "Kendrick Lamar - Topic",
					Duration:   240,
					URL:        "https://youtube.com/watch?v=kendrick",
					Categories: []string{"Music"},
					ViewCount:  65_000_000,
				},
				{
					Title:      "Dr. Dre - Die Hard ft. Eminem",
					Channel:    "Dr. Dre - Topic",
					Duration:   282,
					URL:        "https://youtube.com/watch?v=dre",
					Categories: []string{"Music"},
					ViewCount:  5_000_000,
				},
			},
			wantChannel:  "Kendrick Lamar - Topic",
			wantContains: "Kendrick",
			description:  "must pick Kendrick's Topic channel when artist IS Kendrick",
		},
		{
			name: "topic channel beats vevo for exact match",
			track: TrackRef{
				Title:    "Blinding Lights",
				Artist:   "The Weeknd",
				Duration: 200,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "The Weeknd - Blinding Lights (Official Video)",
					Channel:    "TheWeekndVEVO",
					Duration:   203,
					URL:        "https://youtube.com/watch?v=vevo",
					Categories: []string{"Music"},
					ViewCount:  500_000_000,
				},
				{
					Title:      "Blinding Lights",
					Channel:    "The Weeknd - Topic",
					Duration:   200,
					URL:        "https://youtube.com/watch?v=topic",
					Categories: []string{"Music"},
					ViewCount:  10_000_000,
				},
			},
			wantChannel: "The Weeknd - Topic",
			description: "topic channel is preferred over VEVO",
		},
		{
			name: "title-only match penalized below combined match",
			track: TrackRef{
				Title:    "Lose Yourself",
				Artist:   "Eminem",
				Duration: 326,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "Eminem - Lose Yourself (Official Video)",
					Channel:    "EminemVEVO",
					Duration:   328,
					URL:        "https://youtube.com/watch?v=eminem",
					Categories: []string{"Music"},
					ViewCount:  1_000_000_000,
				},
				{
					Title:      "Lose Yourself - Motivational Speech",
					Channel:    "MotivationHub",
					Duration:   600,
					URL:        "https://youtube.com/watch?v=motivation",
					Categories: []string{"Education"},
					ViewCount:  50_000_000,
				},
			},
			wantContains: "Eminem",
			description:  "combined artist+title match must beat title-only non-music result",
		},
		{
			name: "unrelated candidates filtered out",
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
			},
			wantNil:     true,
			description: "completely unrelated candidates must be filtered by identity threshold",
		},
		{
			name: "feat track prefers candidate naming the featured artist",
			track: TrackRef{
				Title:    "Smaxk Or Die (feat. Playboi Carti)",
				Artist:   "Fatt Smaxk",
				Duration: 0,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "Fatt Smaxk - Smaxk Or Die (Official Music Video) | [Dir. By Kharkee]",
					Channel:    "FattSmaxkVEVO",
					Duration:   150,
					URL:        "https://youtube.com/watch?v=solo",
					Categories: []string{"Music"},
					ViewCount:  2_000_000,
				},
				{
					Title:      "Fatt Smaxk - Smaxk Or Die (feat. Playboi Carti)",
					Channel:    "FattSmaxk",
					Duration:   181,
					URL:        "https://youtube.com/watch?v=feat",
					Categories: []string{"Music"},
					ViewCount:  50_000,
				},
			},
			wantContains: "feat. Playboi Carti",
			description:  "a (feat. X) track must prefer a candidate naming X over an equally-scored, higher-view solo cut",
		},
		{
			name: "same title different artists picks correct one by channel",
			track: TrackRef{
				Title:    "Circles",
				Artist:   "Post Malone",
				Duration: 215,
			},
			candidates: []ports.AudioCandidate{
				{
					Title:      "Circles",
					Channel:    "Post Malone - Topic",
					Duration:   215,
					URL:        "https://youtube.com/watch?v=postmalone",
					Categories: []string{"Music"},
					ViewCount:  20_000_000,
				},
				{
					Title:      "Circles",
					Channel:    "Mac Miller - Topic",
					Duration:   283,
					URL:        "https://youtube.com/watch?v=macmiller",
					Categories: []string{"Music"},
					ViewCount:  15_000_000,
				},
			},
			wantChannel: "Post Malone - Topic",
			description: "when both are Topic channels, prefer the one matching the expected artist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectBest(tt.track, tt.candidates)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q by %q — %s", got.Title, got.Channel, tt.description)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected a candidate, got nil — %s", tt.description)
			}

			if tt.wantChannel != "" && got.Channel != tt.wantChannel {
				t.Errorf("wrong channel: got %q, want %q — %s", got.Channel, tt.wantChannel, tt.description)
			}
			if tt.wantContains != "" && !strings.Contains(got.Title, tt.wantContains) {
				t.Errorf("title %q does not contain %q — %s", got.Title, tt.wantContains, tt.description)
			}
		})
	}
}

func TestAcquisitionMatchingReport(t *testing.T) {
	type testCase struct {
		query       string
		track       TrackRef
		candidates  []ports.AudioCandidate
		wantChannel string
	}

	cases := []testCase{
		{
			query: "Die Hard (Dr. Dre)",
			track: TrackRef{Title: "Die Hard", Artist: "Dr. Dre", Duration: 280},
			candidates: []ports.AudioCandidate{
				{Title: "DIE HARD", Channel: "Kendrick Lamar - Topic", Duration: 240, Categories: []string{"Music"}, ViewCount: 65_000_000},
				{Title: "Die Hard", Channel: "Dr. Dre - Topic", Duration: 282, Categories: []string{"Music"}, ViewCount: 5_000_000},
			},
			wantChannel: "Dr. Dre - Topic",
		},
		{
			query: "Blinding Lights (The Weeknd)",
			track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
			candidates: []ports.AudioCandidate{
				{Title: "The Weeknd - Blinding Lights", Channel: "TheWeekndVEVO", Duration: 203, Categories: []string{"Music"}, ViewCount: 500_000_000},
				{Title: "Blinding Lights", Channel: "The Weeknd - Topic", Duration: 200, Categories: []string{"Music"}, ViewCount: 10_000_000},
			},
			wantChannel: "The Weeknd - Topic",
		},
		{
			query: "Circles (Post Malone)",
			track: TrackRef{Title: "Circles", Artist: "Post Malone", Duration: 215},
			candidates: []ports.AudioCandidate{
				{Title: "Circles", Channel: "Post Malone - Topic", Duration: 215, Categories: []string{"Music"}, ViewCount: 20_000_000},
				{Title: "Circles", Channel: "Mac Miller - Topic", Duration: 283, Categories: []string{"Music"}, ViewCount: 15_000_000},
			},
			wantChannel: "Post Malone - Topic",
		},
	}

	passed, failed := 0, 0
	t.Log("\n=== Acquisition Matching Regression Report ===")
	t.Logf("%-35s %-8s %-25s %s", "TRACK", "STATUS", "SELECTED CHANNEL", "EXPECTED")
	t.Log(strings.Repeat("-", 95))

	for _, tc := range cases {
		got := selectBest(tc.track, tc.candidates)

		var selectedChannel string
		if got != nil {
			selectedChannel = got.Channel
		} else {
			selectedChannel = "(nil)"
		}

		ok := got != nil && got.Channel == tc.wantChannel
		status := "PASS"
		if !ok {
			status = "FAIL"
			failed++
		} else {
			passed++
		}

		t.Logf("%-35s %-8s %-25s %s", tc.query, status, selectedChannel, tc.wantChannel)
	}

	t.Log(strings.Repeat("-", 95))
	t.Logf("Total: %d/%d passed", passed, passed+failed)
}

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
