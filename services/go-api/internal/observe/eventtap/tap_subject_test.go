package eventtap

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPublish_SubjectOmitsUserAuthoredText(t *testing.T) {
	tp := New(events.NewInProcessBus())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.Publish(context.Background(), shared.NewUserId(uuid.New()), "playlist_created", map[string]any{"name": "my private playlist", "title": "a title"})

	evt := <-tap
	if evt.Subject != "" {
		t.Errorf("subject = %q, want empty", evt.Subject)
	}
}
