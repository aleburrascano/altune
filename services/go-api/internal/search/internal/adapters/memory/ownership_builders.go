package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
)

func NewOwnership(t TB, bg *background.Tracker, reader ports.OwnershipReader, filler ports.TrackNumberFiller) *service.OwnershipEnrichmentService {
	t.Helper()
	svc, err := service.NewOwnershipEnrichmentService(bg, reader, filler)
	if err != nil {
		t.Fatalf("inlibrary.NewOwnershipEnrichmentService: %v", err)
	}
	return svc
}
