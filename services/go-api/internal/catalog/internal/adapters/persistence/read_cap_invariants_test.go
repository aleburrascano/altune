package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"testing"
)

func TestReadCaps_ReferenceModuleCap(t *testing.T) {
	if domain.MaxLibraryPageSize != 2000 {
		t.Fatalf("domain.MaxLibraryPageSize = %d, want 2000", domain.MaxLibraryPageSize)
	}
	caps := map[string]int{
		"ownedTrackRefsPageSize": ownedTrackRefsPageSize,
		"maxPlaylistTracks":      maxPlaylistTracks,
		"featuringResultCap":     featuringResultCap,
	}
	for name, got := range caps {
		if got != domain.MaxLibraryPageSize {
			t.Errorf("%s = %d, want domain.MaxLibraryPageSize %d", name, got, domain.MaxLibraryPageSize)
		}
	}
	if maxOwnedTrackRefs != 50_000 {
		t.Errorf("maxOwnedTrackRefs = %d, want the 50000 per-user library ceiling", maxOwnedTrackRefs)
	}
}
