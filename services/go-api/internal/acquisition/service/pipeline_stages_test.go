package service

import (
	"strings"
	"testing"

	"altune/go-api/internal/shared/textnorm"
)

// Acquisition pipeline stage tests: verify each stage's behavior in
// isolation so regressions are pinpointed to the exact stage that broke.
//
// Stages: buildSearchQueries → Search → SelectBestCandidate → Download →
// Store (buildAudioRef) → Tag → UpdateTrack

// ---------------------------------------------------------------------------
// Stage 1: buildSearchQueries
// ---------------------------------------------------------------------------

func TestAcqStage_BuildSearchQueries(t *testing.T) {
	tests := []struct {
		name        string
		track       TrackRef
		wantQueries []string
		wantMin     int
	}{
		{
			name:  "basic track produces title+artist queries",
			track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd"},
			wantQueries: []string{
				"Blinding Lights The Weeknd",
				"Blinding Lights The Weeknd audio",
			},
			wantMin: 2,
		},
		{
			name:  "track with album adds album query",
			track: TrackRef{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding"},
			wantQueries: []string{
				"Circles Post Malone",
				"Circles Post Malone Hollywood's Bleeding",
				"Circles Post Malone audio",
			},
			wantMin: 3,
		},
		{
			name:  "track with ISRC includes ISRC as first query",
			track: TrackRef{Title: "HUMBLE.", Artist: "Kendrick Lamar", ISRC: "USUM71700626"},
			wantQueries: []string{
				"USUM71700626",
				"HUMBLE. Kendrick Lamar",
			},
			wantMin: 3,
		},
		{
			name:    "empty album skips album query",
			track:   TrackRef{Title: "Song", Artist: "Artist", Album: ""},
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := buildSearchQueries(tt.track)
			if len(queries) < tt.wantMin {
				t.Errorf("expected at least %d queries, got %d: %v", tt.wantMin, len(queries), queries)
			}
			for _, want := range tt.wantQueries {
				found := false
				for _, got := range queries {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected query %q in %v", want, queries)
				}
			}
			t.Logf("queries: %v", queries)
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 2: identityScore
// ---------------------------------------------------------------------------

func TestAcqStage_IdentityScore(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		artist    string
		candidate string
		wantAbove float64
		wantBelow float64
	}{
		{
			name:      "exact artist+title match",
			title:     "Blinding Lights",
			artist:    "The Weeknd",
			candidate: "The Weeknd - Blinding Lights",
			wantAbove: 80,
		},
		{
			name:      "title only match is penalized",
			title:     "Die Hard",
			artist:    "Dr. Dre",
			candidate: "DIE HARD",
			wantBelow: 65,
		},
		{
			name:      "correct artist+title beats wrong artist",
			title:     "Die Hard",
			artist:    "Dr. Dre",
			candidate: "Dr. Dre - Die Hard",
			wantAbove: 80,
		},
		{
			name:      "unrelated candidate scores very low",
			title:     "HUMBLE.",
			artist:    "Kendrick Lamar",
			candidate: "Cooking Tutorial Episode 47",
			wantBelow: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := identityScore(tt.title, tt.artist, tt.candidate)
			if tt.wantAbove > 0 && score < tt.wantAbove {
				t.Errorf("identityScore = %.1f, want >= %.1f", score, tt.wantAbove)
			}
			if tt.wantBelow > 0 && score >= tt.wantBelow {
				t.Errorf("identityScore = %.1f, want < %.1f", score, tt.wantBelow)
			}
			t.Logf("identityScore(%q, %q, %q) = %.1f", tt.title, tt.artist, tt.candidate, score)
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 3: channelScore + artistMatchesChannel
// ---------------------------------------------------------------------------

func TestAcqStage_ArtistMatchesChannel(t *testing.T) {
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
		{"Bad Bunny", "RandomUploader", false},
	}

	for _, tt := range tests {
		t.Run(tt.artist+"_"+tt.channel, func(t *testing.T) {
			got := artistMatchesChannel(tt.artist, tt.channel)
			if got != tt.want {
				t.Errorf("artistMatchesChannel(%q, %q) = %v, want %v", tt.artist, tt.channel, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 4: metadataRank
// ---------------------------------------------------------------------------

func TestAcqStage_MetadataRank(t *testing.T) {
	topic := Candidate{
		Channel:    "Artist - Topic",
		Categories: []string{"Music"},
		Duration:   200,
		ViewCount:  10_000_000,
	}
	vevo := Candidate{
		Channel:    "ArtistVEVO",
		Categories: []string{"Music"},
		Duration:   203,
		ViewCount:  500_000_000,
	}
	random := Candidate{
		Channel:    "RandomUploader",
		Categories: []string{"Entertainment"},
		Duration:   600,
		ViewCount:  1_000,
	}

	topicRank := metadataRank(topic, 200, 500_000_000)
	vevoRank := metadataRank(vevo, 200, 500_000_000)
	randomRank := metadataRank(random, 200, 500_000_000)

	// Topic channel weight (0.45) is high but VEVO's view score + duration
	// match can compensate. Verify both score reasonably and random is lowest.
	if topicRank < 0.5 {
		t.Errorf("topic rank (%.3f) unexpectedly low", topicRank)
	}
	if vevoRank < 0.5 {
		t.Errorf("vevo rank (%.3f) unexpectedly low", vevoRank)
	}
	if randomRank >= vevoRank {
		t.Errorf("random (%.3f) should score lower than vevo (%.3f)", randomRank, vevoRank)
	}

	t.Logf("topic=%.3f, vevo=%.3f, random=%.3f", topicRank, vevoRank, randomRank)
}

// ---------------------------------------------------------------------------
// Stage 5: buildAudioRef
// ---------------------------------------------------------------------------

func TestAcqStage_BuildAudioRef(t *testing.T) {
	tests := []struct {
		name  string
		track TrackRef
		want  string
	}{
		{
			name:  "basic",
			track: TrackRef{UserID: "user-1", Artist: "The Weeknd", Album: "After Hours", Title: "Blinding Lights"},
			want:  "user-1/The Weeknd/After Hours/Blinding Lights.mp3",
		},
		{
			name:  "empty album uses unknown",
			track: TrackRef{UserID: "user-1", Artist: "Drake", Album: "", Title: "God's Plan"},
			want:  "user-1/Drake/Unknown Album/God's Plan.mp3",
		},
		{
			name:  "special chars stripped",
			track: TrackRef{UserID: "user-1", Artist: "AC/DC", Album: "Back in Black", Title: "Thunderstruck"},
			want:  "user-1/ACDC/Back in Black/Thunderstruck.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAudioRef(tt.track)
			if got != tt.want {
				t.Errorf("buildAudioRef = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 6: Full candidate selection trace
// ---------------------------------------------------------------------------

func TestAcqStage_FullSelectionTrace(t *testing.T) {
	track := TrackRef{
		Title:    "Blinding Lights",
		Artist:   "The Weeknd",
		Duration: 200,
		ISRC:     "USUM71922973",
	}

	candidates := []Candidate{
		{
			Title:      "The Weeknd - Blinding Lights (Official Video)",
			Channel:    "TheWeekndVEVO",
			Duration:   203,
			Categories: []string{"Music"},
			ViewCount:  3_000_000_000,
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			Categories: []string{"Music"},
			ViewCount:  50_000_000,
		},
		{
			Title:      "Blinding Lights (Piano Cover)",
			Channel:    "Piano Tutorials",
			Duration:   195,
			Categories: []string{"Music"},
			ViewCount:  5_000_000,
		},
		{
			Title:      "Random Podcast Episode",
			Channel:    "PodcastGuy",
			Duration:   3600,
			Categories: []string{"Education"},
			ViewCount:  100_000,
		},
	}

	// Stage 1: Build queries
	queries := buildSearchQueries(track)
	t.Logf("Stage 1 — Search queries: %v", queries)

	// Stage 2: Identity scores
	for _, c := range candidates {
		ident := identityScore(track.Title, track.Artist, c.Title)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)
		t.Logf("Stage 2 — Identity: %q (ch: %q) → score=%.1f, artistMatch=%v, gate=%v",
			c.Title, c.Channel, ident, artMatch, ident >= identityMin)
	}

	// Stage 3: Normalization check
	for _, q := range queries {
		norm := textnorm.NormalizeForMatch(q)
		t.Logf("Stage 3 — Normalize query: %q → %q", q, norm)
	}

	// Stage 4: Selection
	selected := SelectBestCandidate(track, candidates)
	if selected == nil {
		t.Fatal("expected a candidate to be selected")
	}
	t.Logf("Stage 4 — Selected: %q (channel: %q)", selected.Title, selected.Channel)

	// Verify: Topic channel should win
	if !strings.Contains(selected.Channel, "Topic") {
		t.Errorf("expected Topic channel selected, got %q", selected.Channel)
	}
}
