package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"testing"
)

func TestReadCaps_ReferenceModuleCap(t *testing.T) {
	if domain.MaxLibraryPageSize != 2000 {
		t.Fatalf("domain.MaxLibraryPageSize = %d, want 2000", domain.MaxLibraryPageSize)
	}
	if featuringResultCap != domain.MaxLibraryPageSize {
		t.Errorf("featuringResultCap = %d, want domain.MaxLibraryPageSize %d", featuringResultCap, domain.MaxLibraryPageSize)
	}
}
