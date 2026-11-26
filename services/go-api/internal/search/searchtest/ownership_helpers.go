package searchtest

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/inlibrary"
	"altune/go-api/internal/shared/background"
	"testing"
)

func NewOwnership(t testing.TB, bg *background.Tracker, reader ports.OwnershipReader, filler ports.TrackNumberFiller) *inlibrary.OwnershipEnrichmentService {
	t.Helper()
	svc, err := inlibrary.NewOwnershipEnrichmentService(bg, reader, filler)
	if err != nil {
		t.Fatalf("inlibrary.NewOwnershipEnrichmentService: %v", err)
	}
	return svc
}
