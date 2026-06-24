package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestPercentile(t *testing.T) {
	s := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("empty p50 = %d, want 0", got)
	}
	if got := percentile(s, 50); got != 60 {
		t.Errorf("p50 = %d, want 60 (nearest-rank)", got)
	}
	if got := percentile(s, 100); got != 100 {
		t.Errorf("p100 = %d, want 100", got)
	}
}

func TestRunHealthEval_fillAndBridge(t *testing.T) {
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	withArt := track("Humble", "Kendrick", domain.ProviderDeezer, nil)
	withArt.ImageURL = "https://art/1.jpg"
	withArt.Extras = map[string]any{"resolution_tier": domain.EntityResolutionBridge.String()}
	noArt := track("Other", "Kendrick", domain.ProviderITunes, nil) // no image, no bridge

	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {withArt, noArt},
	}}
	r := RunHealthEval(context.Background(), entities, fake, 1, nil)

	if r.Results != 2 {
		t.Fatalf("expected 2 results seen, got %d", r.Results)
	}
	if r.WithArtwork != 1 || r.FillRate != 0.5 {
		t.Errorf("fill: with_artwork=%d rate=%v, want 1 / 0.5", r.WithArtwork, r.FillRate)
	}
	if r.BridgedMerges != 1 || r.BridgeHitRate != 0.5 {
		t.Errorf("bridge: bridged=%d rate=%v, want 1 / 0.5", r.BridgedMerges, r.BridgeHitRate)
	}
	// Health metrics exist for recording but the report is not a HarnessReport
	// (it must never be gated).
	if len(r.HealthMetrics()) != 4 {
		t.Errorf("expected 4 health metrics, got %d", len(r.HealthMetrics()))
	}
}
