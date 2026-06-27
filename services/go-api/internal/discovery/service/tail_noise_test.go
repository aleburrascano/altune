package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestTailNoiseInTopK(t *testing.T) {
	clean := deezerTrack("Clean", "Artist", 50)                       // single curated-catalog source — not noise
	noise := track("type beat", "uploader", domain.ProviderSoundCloud, nil) // single UGC, no identity — noise
	results := []domain.SearchResult{clean, noise, noise, clean}

	if got := TailNoiseInTopK(results, 5); got != 2 {
		t.Errorf("top-5 noise = %d, want 2", got)
	}
	if got := TailNoiseInTopK(results, 1); got != 0 {
		t.Errorf("top-1 noise = %d, want 0 (clean result first)", got)
	}
	if got := TailNoiseInTopK(nil, 5); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}
