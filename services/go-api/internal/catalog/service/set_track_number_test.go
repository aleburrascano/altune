package service

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/domain"
)

func TestSetTrackNumberService(t *testing.T) {
	userId := testUserId()

	t.Run("rejects a non-positive number", func(t *testing.T) {
		svc := NewSetTrackNumberService(newMockTrackRepo())
		if _, err := svc.Execute(context.Background(), userId, domain.NewTrackId(), 0); err == nil {
			t.Fatal("expected an error for a zero track number")
		}
	})

	t.Run("fills an unset position and refuses to overwrite it", func(t *testing.T) {
		repo := newMockTrackRepo()
		svc := NewSetTrackNumberService(repo)
		track := seedTrack(t, repo, userId, "Sicko Mode", "Travis Scott", "ASTROWORLD")

		updated, err := svc.Execute(context.Background(), userId, track.ID, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !updated {
			t.Fatal("expected updated=true on the first fill")
		}
		if got := repo.tracks[track.ID.String()].TrackNumber; got == nil || *got != 3 {
			t.Fatalf("track number = %v, want 3", got)
		}

		// Fill-only: a second call must not overwrite the stored value.
		updated, err = svc.Execute(context.Background(), userId, track.ID, 9)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated {
			t.Fatal("expected updated=false when a number is already set")
		}
		if got := repo.tracks[track.ID.String()].TrackNumber; got == nil || *got != 3 {
			t.Fatalf("track number changed to %v, want it to stay 3", got)
		}
	})
}
