package track

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestGetTrackStatusService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()
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
				track := catalogtest.SeedTrack(t, repo, userId, "Track", "Artist", "Album")
				return track.ID
			},
			wantTitle: "Track",
		},
		{
			name: "non-existent track returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				return domain.NewTrackId()
			},
			wantErr: domain.ErrTrackNotFound,
		},
		{
			name: "track owned by another user returns ErrTrackNotFound",
			setup: func(repo *catalogtest.TrackRepo) domain.TrackId {
				other := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
				track := catalogtest.SeedTrack(t, repo, other, "Track", "Artist", "Album")
				return track.ID
			},
			wantErr: domain.ErrTrackNotFound,
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

func TestListTracksService_RejectsNegativeOffset(t *testing.T) {
	svc := NewListTracksService(catalogtest.NewTrackRepo())

	out, err := svc.Execute(context.Background(), catalogtest.UserId(), domain.LibraryQuery{Offset: -1})

	if err == nil {
		t.Fatalf("expected a validation error for a negative offset, got nil (out=%+v)", out)
	}
	sharedtest.AssertValidationError(t, err)
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
	}
}

func TestListTracksService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()
	errRepo := errors.New("db timeout")

	tests := []struct {
		name        string
		limit       int
		offset      int
		seedCount   int
		setup       func(*catalogtest.TrackRepo)
		wantLen     int
		wantTotal   int
		wantHasMore bool
		wantErr     string
	}{
		{
			name:        "returns tracks with HasMore=true when more exist",
			limit:       2,
			offset:      0,
			seedCount:   3,
			wantLen:     2,
			wantTotal:   3,
			wantHasMore: true,
		},
		{
			name:        "returns tracks with HasMore=false when at end",
			limit:       10,
			offset:      0,
			seedCount:   3,
			wantLen:     3,
			wantTotal:   3,
			wantHasMore: false,
		},
		{
			name:        "default limit applied when zero",
			limit:       0,
			offset:      0,
			seedCount:   2,
			wantLen:     2,
			wantTotal:   2,
			wantHasMore: false,
		},
		{
			name:        "negative limit treated as default",
			limit:       -5,
			offset:      0,
			seedCount:   1,
			wantLen:     1,
			wantTotal:   1,
			wantHasMore: false,
		},
		{
			name:        "limit capped at 2000",
			limit:       5000,
			offset:      0,
			seedCount:   1,
			wantLen:     1,
			wantTotal:   1,
			wantHasMore: false,
		},
		{
			name:      "repo error propagates",
			limit:     10,
			offset:    0,
			seedCount: 0,
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnList = errRepo
			},
			wantErr: "db timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			for i := 0; i < tt.seedCount; i++ {
				catalogtest.SeedTrack(t, repo, userId, "Track "+string(rune('A'+i)), "Artist", "Album")
			}
			svc := NewListTracksService(repo)

			out, err := svc.Execute(ctx, userId, domain.LibraryQuery{Limit: tt.limit, Offset: tt.offset})

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
			if len(out.Tracks) != tt.wantLen {
				t.Errorf("len(Tracks) = %d, want %d", len(out.Tracks), tt.wantLen)
			}
			if out.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", out.Total, tt.wantTotal)
			}
			if out.HasMore != tt.wantHasMore {
				t.Errorf("HasMore = %v, want %v", out.HasMore, tt.wantHasMore)
			}
		})
	}
}
