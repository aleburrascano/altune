package service

import (
	"fmt"
	"strings"
	"testing"
)

// TestAcquisitionMatchingRegression validates that the candidate selection
// algorithm picks the correct YouTube result for a known set of tracks.
// Each test case simulates the candidates that yt-dlp would return and
// asserts which candidate is selected.
//
// Run with: go test ./internal/acquisition/service/... -run TestAcquisitionMatchingRegression -v
func TestAcquisitionMatchingRegression(t *testing.T) {
	tests := []struct {
		name         string
		track        TrackRef
		candidates   []Candidate
		wantNil      bool
		wantChannel  string
		wantContains string // substring in selected candidate's title
		description  string
	}{
		{
			name: "die hard by intended artist prefers correct Topic channel",
			track: TrackRef{
				Title:    "Die Hard",
				Artist:   "Dr. Dre",
				Duration: 280,
			},
			candidates: []Candidate{
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
			candidates: []Candidate{
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
			candidates: []Candidate{
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
			candidates: []Candidate{
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
			candidates: []Candidate{
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
			name: "same title different artists picks correct one by channel",
			track: TrackRef{
				Title:    "Circles",
				Artist:   "Post Malone",
				Duration: 215,
			},
			candidates: []Candidate{
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
			got := SelectBestCandidate(tt.track, tt.candidates)

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

// TestAcquisitionMatchingReport prints a summary of all regression cases.
// Run with: go test ./internal/acquisition/service/... -run TestAcquisitionMatchingReport -v
func TestAcquisitionMatchingReport(t *testing.T) {
	type testCase struct {
		query       string
		track       TrackRef
		candidates  []Candidate
		wantChannel string
	}

	cases := []testCase{
		{
			query: "Die Hard (Dr. Dre)",
			track: TrackRef{Title: "Die Hard", Artist: "Dr. Dre", Duration: 280},
			candidates: []Candidate{
				{Title: "DIE HARD", Channel: "Kendrick Lamar - Topic", Duration: 240, Categories: []string{"Music"}, ViewCount: 65_000_000},
				{Title: "Die Hard", Channel: "Dr. Dre - Topic", Duration: 282, Categories: []string{"Music"}, ViewCount: 5_000_000},
			},
			wantChannel: "Dr. Dre - Topic",
		},
		{
			query: "Blinding Lights (The Weeknd)",
			track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
			candidates: []Candidate{
				{Title: "The Weeknd - Blinding Lights", Channel: "TheWeekndVEVO", Duration: 203, Categories: []string{"Music"}, ViewCount: 500_000_000},
				{Title: "Blinding Lights", Channel: "The Weeknd - Topic", Duration: 200, Categories: []string{"Music"}, ViewCount: 10_000_000},
			},
			wantChannel: "The Weeknd - Topic",
		},
		{
			query: "Circles (Post Malone)",
			track: TrackRef{Title: "Circles", Artist: "Post Malone", Duration: 215},
			candidates: []Candidate{
				{Title: "Circles", Channel: "Post Malone - Topic", Duration: 215, Categories: []string{"Music"}, ViewCount: 20_000_000},
				{Title: "Circles", Channel: "Mac Miller - Topic", Duration: 283, Categories: []string{"Music"}, ViewCount: 15_000_000},
			},
			wantChannel: "Post Malone - Topic",
		},
	}

	passed, failed := 0, 0
	t.Log("\n=== Acquisition Matching Regression Report ===")
	t.Log(fmt.Sprintf("%-35s %-8s %-25s %s", "TRACK", "STATUS", "SELECTED CHANNEL", "EXPECTED"))
	t.Log(strings.Repeat("-", 95))

	for _, tc := range cases {
		got := SelectBestCandidate(tc.track, tc.candidates)

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

		t.Log(fmt.Sprintf("%-35s %-8s %-25s %s", tc.query, status, selectedChannel, tc.wantChannel))
	}

	t.Log(strings.Repeat("-", 95))
	t.Log(fmt.Sprintf("Total: %d/%d passed", passed, passed+failed))
}
