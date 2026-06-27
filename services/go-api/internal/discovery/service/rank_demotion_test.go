package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// multiSource returns r with a second provider source appended (corroborated).
func multiSource(r domain.SearchResult, p domain.ProviderName) domain.SearchResult {
	r.Sources = append(r.Sources, domain.SourceRef{Provider: p, ExternalID: "x", URL: "https://x"})
	return r
}

func TestIsLowConfidenceTail(t *testing.T) {
	tests := []struct {
		name string
		r    domain.SearchResult
		want bool
	}{
		{
			name: "single-source soundcloud, no identity → demoted",
			r:    track("che rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil),
			want: true,
		},
		{
			name: "single-source lastfm, no identity → demoted",
			r:    track("Intro", "che rest in bass", domain.ProviderLastFM, nil),
			want: true,
		},
		{
			name: "single-source soundcloud but carries album → rescued",
			r:    track("Real Song", "Artist", domain.ProviderSoundCloud, map[string]any{"album": "Real Album"}),
			want: false,
		},
		{
			name: "single-source soundcloud but carries isrc → rescued",
			r:    track("Real Song", "Artist", domain.ProviderSoundCloud, map[string]any{"isrc": "US1234567890"}),
			want: false,
		},
		{
			name: "single-source deezer (curated catalog) → never demoted",
			r:    track("Real Song", "Artist", domain.ProviderDeezer, nil),
			want: false,
		},
		{
			name: "single-source itunes (curated catalog) → never demoted",
			r:    track("Real Song", "Artist", domain.ProviderITunes, nil),
			want: false,
		},
		{
			name: "multi-source soundcloud+lastfm (corroborated) → never demoted",
			r:    multiSource(track("BA$$", "che", domain.ProviderSoundCloud, nil), domain.ProviderLastFM),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLowConfidenceTail(tt.r); got != tt.want {
				t.Errorf("isLowConfidenceTail = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRankWith_DemotesUGCNoiseBelowCleanResult(t *testing.T) {
	// The junk title stuffs the distinguishing query tokens (rest/in/bass) → high
	// relevance. The clean catalog result shares only the common word, so it has
	// LOWER relevance — yet demotion must still lift it above the UGC noise. This is
	// the strong claim: demotion overrides a genuine relevance advantage.
	junk := track("rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil)
	clean := deezerTrack("Encore", "Che", 10)

	// Baseline (no demotion): the query-word-stuffed junk wins on relevance.
	base := Rank([]Entity{ent(clean), ent(junk)}, "rest in bass encore")
	if len(base) != 2 || base[0].Subtitle != "prodguy" {
		t.Fatalf("baseline precondition: expected junk first on relevance, got %v", titles(base))
	}

	// With demotion: the corroborated catalog result rises despite lower relevance.
	got := rankWith([]Entity{ent(clean), ent(junk)}, "rest in bass encore", isLowConfidenceTail, nil)
	if len(got) != 2 || got[0].Subtitle != "Che" {
		t.Fatalf("with demotion: expected clean 'Che' first, got %v", titles(got))
	}
	if got[1].Subtitle != "prodguy" {
		t.Fatalf("with demotion: expected UGC noise last, got %v", titles(got))
	}
}

func TestRankWith_NilPredicateMatchesRank(t *testing.T) {
	// rankWith(nil) must be byte-identical in ordering to Rank — the inertness that
	// keeps the default path and the sacred tests unchanged.
	junk := track("crazy bootleg edit", "uploader", domain.ProviderSoundCloud, nil)
	clean := deezerTrack("Crazy", "Gnarls Barkley", 50)
	entities := []Entity{ent(clean), ent(junk)}

	a := Rank(entities, "crazy")
	b := rankWith(entities, "crazy", nil, nil)
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %v vs %v", titles(a), titles(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Subtitle != b[i].Subtitle {
			t.Fatalf("ordering mismatch at %d: %v vs %v", i, titles(a), titles(b))
		}
	}
}
