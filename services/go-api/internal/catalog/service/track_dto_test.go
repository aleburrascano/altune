package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// The storage key embeds the owner's user id; it must never reach a client in
// a REST track response or the track_added SSE payload built from the DTO.
func TestTrackToDTO_DoesNotSerializeAudioRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	audioRef := userId.String() + "/m83/hurry-up/midnight-city.opus"
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("mark ready: %v", err)
	}

	dto := TrackToDTO(track)
	if dto.AudioRef == nil || *dto.AudioRef != audioRef {
		t.Fatalf("internal AudioRef = %v, want %q kept for server-side use", dto.AudioRef, audioRef)
	}

	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "audio_ref") {
		t.Errorf("TrackDTO JSON exposes audio_ref: %s", body)
	}
	if strings.Contains(body, userId.String()) {
		t.Errorf("TrackDTO JSON leaks the owner's user id: %s", body)
	}

	if _, ok := trackAddedPayload(track)["audio_ref"]; ok {
		t.Error("track_added payload exposes audio_ref")
	}
}
