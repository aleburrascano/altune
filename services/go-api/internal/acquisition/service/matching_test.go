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
			Title:      "Blinding Lights",
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

func TestQualifierDistance_HandlesFullwidthBrackets(t *testing.T) {
	cases := []struct {
		name  string
		track string
	}{
		{"ascii brackets", "Song (Live)"},
		{"fullwidth brackets", "Song （Live）"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := qualifierDistance("Song", c.track); got == 0 {
				t.Errorf("qualifierDistance(%q, %q) = 0, want an unrequested marker cost", "Song", c.track)
			}
		})
	}
}

func joinQualifiers(labels []string) string {
	return strings.Join(labels, "|")
}

func assertQualifiers(t *testing.T, name string, got, want []string) {
	t.Helper()
	if joinQualifiers(got) != joinQualifiers(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestUnrequestedQualifiers_InstrumentalAndAccuracyClaimAreVetoed(t *testing.T) {
	veto, fallback := UnrequestedQualifiers("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instrumental) [100% Accurate]")
	assertQualifiers(t, "veto", veto, []string{"instrumental", "100% accurate"})
	assertQualifiers(t, "fallback", fallback, nil)
}

func TestUnrequestedQualifiers_ReactionVideoIsVetoed(t *testing.T) {
	veto, fallback := UnrequestedQualifiers("8AM In Charlotte", "Drake", "ImDontai Reacts To Drake 8AM In Charlotte")
	assertQualifiers(t, "veto", veto, []string{"reacts"})
	assertQualifiers(t, "fallback", fallback, nil)
}

func TestUnrequestedQualifiers_RadioEditIsFallbackNotEdit(t *testing.T) {
	veto, fallback := UnrequestedQualifiers("Song", "Someone", "Song (Radio Edit)")
	assertQualifiers(t, "veto", veto, nil)
	assertQualifiers(t, "fallback", fallback, []string{"radio edit"})
}

func TestUnrequestedQualifiers_RequestedWordInTrackTitleIsNotAQualifier(t *testing.T) {
	veto, fallback := UnrequestedQualifiers("Live Forever", "Oasis", "Live Forever (Remastered)")
	assertQualifiers(t, "veto", veto, nil)
	assertQualifiers(t, "fallback", fallback, nil)
}

func TestUnrequestedQualifiers_RequestedWordAtEndOfTrackTitleIsNotAQualifier(t *testing.T) {
	veto, fallback := UnrequestedQualifiers("Forever Live", "Oasis", "Forever Live (Remastered)")
	assertQualifiers(t, "veto", veto, nil)
	assertQualifiers(t, "fallback", fallback, nil)
}

func TestQualifierDistance_CountsEachUnrequestedBracketSegment(t *testing.T) {
	if got := qualifierDistance("Song", "Song (Acoustic) (Live)"); got != 2*unrequestedQualifierCost {
		t.Errorf("qualifierDistance with two unrequested bracket segments = %d, want %d", got, 2*unrequestedQualifierCost)
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

	ranked, _ := rankAndCollect(context.Background(), track, candidates)
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

	ranked, _ := rankAndCollect(context.Background(), track, candidates)
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

	ranked, _ := rankAndCollect(context.Background(), track, candidates)
	if ranked[0].Channel != "TheWeekndVEVO" {
		t.Fatalf("selected %q — off Topic, label provenance outranks a shorter qualifier list", ranked[0].Channel)
	}
}

func TestUnrequestedQualifiers(t *testing.T) {
	tests := []struct {
		name          string
		title, artist string
		candidate     string
		wantVeto      []string
		wantFallback  []string
	}{
		{"unbracketed slowed and reverb", "Song", "Someone", "Someone - Song slowed + reverb", []string{"slowed", "reverb"}, nil},
		{"punctuation folded", "Song", "Someone", "Song 100 accurate", []string{"100% accurate"}, nil},
		{"fullwidth brackets", "Song", "Someone", "Song （Live）", []string{"live"}, nil},
		{"remixed labelled remix", "Song", "Someone", "Song remixed", []string{"remix"}, nil},
		{"mix is a whole word", "Song", "Someone", "Song (Remix) mix", []string{"remix", "mix"}, nil},
		{"remix does not match mix", "Song", "Someone", "Song (Remix)", []string{"remix"}, nil},
		{"acapella spellings share a label", "Song", "Someone", "Song acapella (A Cappella)", []string{"a cappella"}, nil},
		{"title order and deduplicated", "Song", "Someone", "Song (Live) [Snippet] live leaked leak", []string{"live", "snippet", "leak"}, nil},
		{"phrases", "Song", "Someone", "Someone Type Beat - Song In The Booth Sped Up 8D", []string{"type beat", "in the booth", "sped up", "8d"}, nil},
		{"lone phrase word is not the phrase", "Song", "Someone", "Song in the studio type", nil, nil},
		{"veto and fallback split", "Song", "Someone", "Song (Extended Version) [Karaoke]", []string{"karaoke"}, []string{"extended", "version"}},
		{"bare edit is fallback", "Song", "Someone", "Song (Edit)", nil, []string{"edit"}},
		{"artist word is requested", "Song", "Live", "Live - Song", nil, nil},
		{"title phrase is requested", "Slowed Down", "Someone", "Slowed Down (Reverb)", []string{"reverb"}, nil},
		{"requested qualifier in title brackets", "Song (Remix)", "Someone", "Song (Remix)", nil, nil},
		{"feature credit is never a qualifier", "Song", "Someone", "Song (feat. Live Cover Band)", nil, nil},
		{"unbracketed feature credit", "Song", "Someone", "Song ft. Live Band", nil, nil},
		{"feature credit stops at a separator", "Song", "Someone", "Song ft. Other - Instrumental", []string{"instrumental"}, nil},
		{"fullwidth feature credit stops at its bracket", "Song", "Someone", "Song （feat. Other） （Instrumental）", []string{"instrumental"}, nil},
		{"remastered is not a qualifier", "Song", "Someone", "Song (Remastered 2011)", nil, nil},
		{"empty candidate", "Song", "Someone", "", nil, nil},
		{"empty track", "", "", "Song (Cover)", []string{"cover"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			veto, fallback := UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
			assertQualifiers(t, "veto of "+tt.candidate, veto, tt.wantVeto)
			assertQualifiers(t, "fallback of "+tt.candidate, fallback, tt.wantFallback)
		})
	}
}

func FuzzUnrequestedQualifiers(f *testing.F) {
	f.Add("Rollacoasta", "prettifun", "prettifun - Rollacoasta (Instrumental) [100% Accurate]")
	f.Add("8AM In Charlotte", "Drake", "ImDontai Reacts To Drake 8AM In Charlotte")
	f.Add("Song", "Someone", "Song （feat. Other） （Radio Edit） a")
	labels := make(map[string]bool)
	for _, entry := range qualifierLexicon {
		labels[entry.label] = true
	}
	f.Fuzz(func(t *testing.T, title, artist, candidate string) {
		veto, fallback := UnrequestedQualifiers(title, artist, candidate)
		seen := make(map[string]bool)
		for _, label := range append(veto, fallback...) {
			if !labels[label] || seen[label] {
				t.Fatalf("UnrequestedQualifiers(%q, %q, %q) = %v, %v: %q is unknown or repeated", title, artist, candidate, veto, fallback, label)
			}
			seen[label] = true
		}
	})
}

func TestUnrequestedQualifiers_TitlesFromTheOutsideWorld(t *testing.T) {
	longCandidate := "Song" + strings.Repeat(" (Live)", 5000) + " [Instrumental]"
	tests := []struct {
		name          string
		title, artist string
		candidate     string
		wantVeto      []string
		wantFallback  []string
	}{
		{"all caps instrumental upload is vetoed", "SPEED DEMON", "Lucy Bedroque", "Lucy Bedroque - SPEED DEMON (INSTRUMENTAL)", []string{"instrumental"}, nil},
		{"lower case track title still requests the word", "live forever", "oasis", "LIVE FOREVER (LIVE)", nil, nil},
		{"veto order follows the title not the lexicon", "Song", "Someone", "Song [Karaoke] (Instrumental) reverb slowed", []string{"karaoke", "instrumental", "reverb", "slowed"}, nil},
		{"fullwidth letters are folded", "Song", "Someone", "Song （ＬＩＶＥ）", []string{"live"}, nil},
		{"lenticular brackets are separators", "Song", "Someone", "Song【Instrumental】", []string{"instrumental"}, nil},
		{"corner brackets are separators", "Song", "Someone", "Song「Live」", []string{"live"}, nil},
		{"en dash is a separator", "Song", "Someone", "Song–Instrumental", []string{"instrumental"}, nil},
		{"unclosed bracket still carries its qualifier", "Song", "Someone", "Song (Instrumental", []string{"instrumental"}, nil},
		{"stray closing bracket still carries its qualifier", "Song", "Someone", "Song Instrumental)", []string{"instrumental"}, nil},
		{"nested brackets carry both qualifiers", "Song", "Someone", "Song [Live (Instrumental)]", []string{"live", "instrumental"}, nil},
		{"hyphenated sped up", "Song", "Someone", "Song (sped-up)", []string{"sped up"}, nil},
		{"accuracy claim without a space", "Song", "Someone", "Song [100%Accurate]", []string{"100% accurate"}, nil},
		{"live version splits into both families", "Song", "Someone", "Song (Live Version)", []string{"live"}, []string{"version"}},
		{"repeated qualifier in mixed case is reported once", "Song", "Someone", "Song (LIVE) live Live", []string{"live"}, nil},
		{"live inside a longer word is not live", "Song", "Someone", "Song (Livestream Audio)", nil, nil},
		{"mix inside a longer word is not mix", "Song", "Someone", "Song (Mixtape Version)", nil, []string{"version"}},
		{"8d inside a longer token is not 8d", "Song", "Someone", "Song 18d", nil, nil},
		{"title containing live as a substring does not request live", "Delivery", "Someone", "Delivery (Live)", []string{"live"}, nil},
		{"title word mix does not request remix", "Mix Tape", "Someone", "Mix Tape (Remix)", []string{"remix"}, nil},
		{"nightcore uploader prefix is vetoed", "Song", "Someone", "Nightcore - Song", []string{"nightcore"}, nil},
		{"very long candidate is reported once per qualifier", "Song", "Someone", longCandidate, []string{"live", "instrumental"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			veto, fallback := UnrequestedQualifiers(tt.title, tt.artist, tt.candidate)
			assertQualifiers(t, "veto", veto, tt.wantVeto)
			assertQualifiers(t, "fallback", fallback, tt.wantFallback)
		})
	}
}

func TestUnrequestedQualifiers_DotCommaAndUnderscoreAreSeparators(t *testing.T) {
	tests := []struct {
		candidate string
		wantVeto  []string
	}{
		{"Song_Instrumental", []string{"instrumental"}},
		{"Song.Instrumental", []string{"instrumental"}},
		{"Song,Live", []string{"live"}},
		{"Song...Live", []string{"live"}},
		{"Song (Slowed.Reverb)", []string{"slowed", "reverb"}},
		{"Song (Slowed_Reverb)", []string{"slowed", "reverb"}},
	}
	for _, tt := range tests {
		t.Run(tt.candidate, func(t *testing.T) {
			veto, _ := UnrequestedQualifiers("Song", "Someone", tt.candidate)
			assertQualifiers(t, "veto", veto, tt.wantVeto)
		})
	}
}

func TestUnrequestedQualifiers_UnicodeDashSeparatorsAfterFeatureCredit(t *testing.T) {
	tests := []struct {
		name      string
		separator rune
	}{
		{"hyphen", '‐'},
		{"non-breaking hyphen", '‑'},
		{"figure dash", '‒'},
		{"horizontal bar", '―'},
		{"minus sign", '−'},
		{"small em dash", '﹘'},
		{"small hyphen-minus", '﹣'},
		{"fullwidth hyphen-minus", '－'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := "Someone " + string(tt.separator) + " Song ft. Other " + string(tt.separator) + " Instrumental"
			veto, _ := UnrequestedQualifiers("Song", "Someone", candidate)
			assertQualifiers(t, "veto", veto, []string{"instrumental"})
		})
	}
}

func TestUnrequestedQualifiers_RemasterStyleMixIsNotAQualifier(t *testing.T) {
	for _, title := range []string{
		"Come Together (2019 Mix)",
		"Come Together (Original Mix)",
		"Come Together (Stereo Mix)",
		"Come Together (Mono Mix)",
		"Come Together (Album Mix)",
	} {
		veto, fallback := UnrequestedQualifiers("Come Together", "The Beatles", title)
		assertQualifiers(t, title+" veto", veto, nil)
		assertQualifiers(t, title+" fallback", fallback, nil)
	}
}

func TestUnrequestedQualifiers_OtherMixesStayVetoed(t *testing.T) {
	for _, title := range []string{"Song (Who Mix?)", "Song (Club Mix)"} {
		veto, _ := UnrequestedQualifiers("Song", "A", title)
		assertQualifiers(t, title+" veto", veto, []string{"mix"})
	}
}

func TestRankAndCollect_RejectsUnrequestedVersionBeforeDownload(t *testing.T) {
	track := TrackRef{Title: "Rollacoasta", Artist: "prettifun", Duration: 159}
	candidates := []ports.AudioCandidate{
		{
			Title:      "prettifun - Rollacoasta (Instrumental) [100% Accurate]",
			Channel:    "ProdADN",
			Duration:   159,
			URL:        "https://youtube.com/watch?v=instrumental",
			Categories: []string{"Music"},
		},
		{Title: "Rollacoasta", Channel: "prettifun", Duration: 159, URL: "https://youtube.com/watch?v=clean", Categories: []string{"Music"}},
	}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(ranked) != 1 || ranked[0].URL != "https://youtube.com/watch?v=clean" {
		t.Fatalf("ranked = %v, want only the clean upload", ranked)
	}
	if len(rejected) != 1 || rejected[0].Stage != RejectionQualifier || rejected[0].Reason != "unrequested instrumental, 100% accurate" {
		t.Fatalf("rejected = %+v, want one qualifier rejection naming its labels", rejected)
	}
}

func TestRankAndCollect_RadioEditRanksAfterEveryCleanCandidate(t *testing.T) {
	track := TrackRef{Title: "Harbour Lights", Artist: "The Marram", Duration: 236}
	candidates := []ports.AudioCandidate{
		{Title: "Harbour Lights (Radio Edit)", Channel: "The Marram - Topic", Duration: 236, URL: "https://x.example/edit", Categories: []string{"Music"}},
		{Title: "Harbour Lights", Channel: "The Marram - Topic", Duration: 200, URL: "https://x.example/clean", Categories: []string{"Music"}},
	}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(rejected) != 0 || len(ranked) != 2 || ranked[0].URL != "https://x.example/clean" {
		t.Fatalf("ranked = %v rejected = %v, want the clean upload first and the edit kept", ranked, rejected)
	}
}

func TestRankAndCollect_MustHold8_UnplayableCandidatesAreRejectedBeforeRanking(t *testing.T) {
	track := TrackRef{Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 240}
	candidates := []ports.AudioCandidate{
		{Title: "Drinking in L.A.", URL: "sc:drm", Unplayable: "drm"},
		{Title: "Drinking in L.A.", URL: "sc:preview", Unplayable: "preview"},
		{Title: "Drinking in L.A.", URL: "sc:ok"},
	}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(ranked) != 1 || ranked[0].URL != "sc:ok" {
		t.Fatalf("ranked = %v, want only the playable candidate so the downloader never sees the others", ranked)
	}
	if len(rejected) != 2 || rejected[0].Stage != RejectionDRM || rejected[1].Stage != RejectionPreview {
		t.Fatalf("rejected = %v, want drm then preview", rejected)
	}
}
