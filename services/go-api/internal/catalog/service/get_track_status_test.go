package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestGetTrackStatusService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name      string
		setup     func(*catalogtest.TrackRepo) domain.TrackId
		wantTitle string
		wantErr   error
	}{
		{
			name: "existing track is returned",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantTitle: "Track",
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "track owned by another user returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				other := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
				track := seedTrack(t, repo, other, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				repo.ErrOnGetBy = errRepo
				return domain.NewTrackId()
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			trackId := tt.setup(repo)
			svc := NewGetTrackStatusService(repo)

			track, err := svc.Execute(ctx, userId, trackId)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track == nil {
				t.Fatal("expected non-nil track")
			}
			if track.ID != trackId {
				t.Errorf("track.ID = %v, want %v", track.ID, trackId)
			}
			if track.Title != tt.wantTitle {
				t.Errorf("track.Title = %q, want %q", track.Title, tt.wantTitle)
			}
		})
	}
}
