package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAddTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db connection lost")

	tests := []struct {
		name        string
		input       AddTrackInput
		setup       func(*catalogtest.TrackRepo)
		wantCreated bool
		wantTitle   string
		wantErr     string
	}{
		{
			name: "new track is created",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			wantCreated: true,
			wantTitle:   "Track",
		},
		{
			name: "duplicate returns existing track not created",
			input: AddTrackInput{
				Title:  "Existing",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				seedTrack(t, repo, userId, "Existing", "Artist", "Album")
			},
			wantCreated: false,
			wantTitle:   "Existing",
		},
		{
			name: "empty title returns validation error",
			input: AddTrackInput{
				Title:  "",
				Artist: "Artist",
				Album:  "Album",
			},
			wantErr: "track title required",
		},
		{
			name: "empty artist returns validation error",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "",
				Album:  "Album",
			},
			wantErr: "track artist required",
		},
		{
			name: "repo error propagates",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := NewAddTrackService(repo)

			out, err := svc.Execute(ctx, userId, tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Created != tt.wantCreated {
				t.Errorf("Created = %v, want %v", out.Created, tt.wantCreated)
			}
			if out.Track == nil {
				t.Fatal("expected non-nil Track in output")
			}
			if out.Track.Title != tt.wantTitle {
				t.Errorf("Track.Title = %q, want %q", out.Track.Title, tt.wantTitle)
			}
		})
	}
}
