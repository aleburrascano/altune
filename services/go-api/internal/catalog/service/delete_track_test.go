package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDeleteTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db error")

	tests := []struct {
		name    string
		setup   func(*catalogtest.TrackRepo) domain.TrackId
		wantErr error
	}{
		{
			name: "existing track is deleted",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr: nil,
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr: ErrTrackNotFound,
		},
		{
			name: "repo error propagates",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				track := seedTrack(t, repo, userId, "Track", "Artist", "Album")
				repo.ErrOnDelete = errRepo
				return track.ID
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			trackId := tt.setup(repo)
			svc := NewDeleteTrackService(repo, catalogtest.NewAudioStore())

			err := svc.Execute(ctx, userId, trackId)

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
		})
	}
}
