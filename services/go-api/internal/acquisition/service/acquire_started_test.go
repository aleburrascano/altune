package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// TestExecute_PublishesStartedEvent is the F7/F8 regression: acquisition must
// emit a server-authoritative track_acquisition_started event when it begins, so
// the client can seed the download UI (and flip a re-acquired ready/failed track
// back to pending) without depending on the optimistic save or the poll.
func TestExecute_PublishesStartedEvent(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	pub := &recordingProgressPublisher{}
	svc := NewAcquireTrackAudioService(
		repo,
		&fakeAudioSearcher{}, // no results → pipeline fails after started fires
		newFakeAudioStore(),
		WithAcquireEvents(pub),
	)

	_ = svc.Execute(context.Background(), userId, track.ID, "")

	var started *recordedProgress
	for i := range pub.events {
		if pub.events[i].typ == "track_acquisition_started" {
			started = &pub.events[i]
			break
		}
	}
	if started == nil {
		t.Fatalf("no track_acquisition_started event published; got %+v", pub.events)
	}
	if started.payload["track_id"] != track.ID.String() {
		t.Fatalf("started track_id = %v, want %s", started.payload["track_id"], track.ID.String())
	}
}
