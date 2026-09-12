package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestListTracksService_RejectsNegativeOffset(t *testing.T) {
	svc := NewListTracksService(catalogtest.NewTrackRepo())

	out, err := svc.Execute(context.Background(), testUserId(), domain.LibraryQuery{Offset: -1})

	if err == nil {
		t.Fatalf("expected a validation error for a negative offset, got nil (out=%+v)", out)
	}
	shared.AssertValidationError(t, err)
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
	}
}

func TestListTracksService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
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
				seedTrack(t, repo, userId, "Track "+string(rune('A'+i)), "Artist", "Album")
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
