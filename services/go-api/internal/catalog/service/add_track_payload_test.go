package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// TestTrackAddedPayload is the F10 regression: the track_added_to_library event
// must embed the full track (wire-shaped) so a receiving client inserts the row
// directly instead of forcing a refetch.
func TestTrackAddedPayload(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	p := trackAddedPayload(context.Background(), track)

	if p["id"] != track.ID.String() {
		t.Errorf("id = %v, want %s", p["id"], track.ID.String())
	}
	if p["track_id"] != track.ID.String() {
		t.Errorf("track_id (legacy) = %v, want %s", p["track_id"], track.ID.String())
	}
	if p["title"] != "Midnight City" || p["artist"] != "M83" {
		t.Errorf("title/artist = %v/%v", p["title"], p["artist"])
	}
	// The payload is the marshaled TrackDTO, so values carry JSON types.
	if album, ok := p["album"].(string); !ok || album != "Hurry Up, We're Dreaming" {
		t.Errorf("album = %v, want the album string", p["album"])
	}
	if p["acquisition_status"] != track.AcquisitionStatus.String() {
		t.Errorf("acquisition_status = %v", p["acquisition_status"])
	}
}

func TestTrackAddedPayload_EmptyAlbumIsNil(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Single", "Artist", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	if album := trackAddedPayload(context.Background(), track)["album"]; album != nil {
		t.Errorf("empty album = %v, want JSON null", album)
	}
}
