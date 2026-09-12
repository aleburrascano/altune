package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"strings"
	"testing"
)

func TestSetTrackNumberService(t *testing.T) {
	userId := testUserId()

	t.Run("rejects a non-positive number", func(t *testing.T) {
		svc := NewSetTrackNumberService(catalogtest.NewTrackRepo())
		if _, err := svc.Execute(context.Background(), userId, domain.NewTrackId(), 0); err == nil {
			t.Fatal("expected an error for a zero track number")
		}
	})

	t.Run("rejects an out-of-range number", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		svc := NewSetTrackNumberService(repo)
		_, err := svc.Execute(context.Background(), userId, domain.NewTrackId(), 3000000000)
		if err == nil {
			t.Fatal("expected a validation error for an out-of-range track number")
		}
		shared.AssertValidationError(t, err)
		if !strings.Contains(err.Error(), "track_number") {
			t.Fatalf("error = %q, want it to mention %q", err.Error(), "track_number")
		}
	})

	t.Run("fills an unset position and refuses to overwrite it", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		svc := NewSetTrackNumberService(repo)
		track := seedTrack(t, repo, userId, "Sicko Mode", "Travis Scott", "ASTROWORLD")

		updated, err := svc.Execute(context.Background(), userId, track.ID, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !updated {
			t.Fatal("expected updated=true on the first fill")
		}
		if got := repo.Tracks[track.ID.String()].TrackNumber; got == nil || *got != 3 {
			t.Fatalf("track number = %v, want 3", got)
		}

		updated, err = svc.Execute(context.Background(), userId, track.ID, 9)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated {
			t.Fatal("expected updated=false when a number is already set")
		}
		if got := repo.Tracks[track.ID.String()].TrackNumber; got == nil || *got != 3 {
			t.Fatalf("track number changed to %v, want it to stay 3", got)
		}
	})
}
