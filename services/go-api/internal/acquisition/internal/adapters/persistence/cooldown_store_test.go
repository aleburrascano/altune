package persistence_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"testing"

	"github.com/google/uuid"
)

func failedTrack(_ *testing.T) *acquisition.TrackSnapshot {
	return &acquisition.TrackSnapshot{
		ID:     shared.NewTrackId(),
		UserId: shared.NewUserId(uuid.New()),
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Status: acquisition.TrackFailed,
	}
}
