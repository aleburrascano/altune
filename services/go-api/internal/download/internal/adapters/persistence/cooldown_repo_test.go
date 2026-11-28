package persistence_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/google/uuid"
)

func failedTrack(_ *testing.T) *domain.TrackSnapshot {
	return &domain.TrackSnapshot{
		ID:     ids.NewTrackId(),
		UserId: ids.NewUserId(uuid.New()),
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Status: domain.TrackFailed,
	}
}
