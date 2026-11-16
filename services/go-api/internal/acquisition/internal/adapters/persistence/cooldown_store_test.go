package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	shared "altune/go-api/internal/shared/kernel"
	"testing"

	"github.com/google/uuid"
)

func failedTrack(_ *testing.T) *ports.TrackSnapshot {
	return &ports.TrackSnapshot{
		ID:     shared.NewTrackId(),
		UserId: shared.NewUserId(uuid.New()),
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Status: ports.TrackFailed,
	}
}
