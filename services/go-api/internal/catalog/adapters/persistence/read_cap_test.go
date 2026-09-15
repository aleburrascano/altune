package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"testing"
)

// TestReadCaps_ReferenceModuleCap guards #1034: every adapter-side row bound
// derives from the single domain.MaxLibraryPageSize, whose value stays 2000.
func TestReadCaps_ReferenceModuleCap(t *testing.T) {
	if domain.MaxLibraryPageSize != 2000 {
		t.Fatalf("domain.MaxLibraryPageSize = %d, want 2000", domain.MaxLibraryPageSize)
	}
	caps := map[string]int{
		"maxOwnedTrackRefs":  maxOwnedTrackRefs,
		"maxPlaylistTracks":  maxPlaylistTracks,
		"featuringResultCap": featuringResultCap,
	}
	for name, got := range caps {
		if got != domain.MaxLibraryPageSize {
			t.Errorf("%s = %d, want domain.MaxLibraryPageSize %d", name, got, domain.MaxLibraryPageSize)
		}
	}
}
