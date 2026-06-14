package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestListSearchHistoryService_Execute(t *testing.T) {
	userID := shared.NewUserId(uuid.New())

	fixedEntries := []*domain.SearchHistoryEntry{
		{
			ID:        uuid.New(),
			UserId:    userID,
			Query:     "radiohead",
			QueryNorm: "radiohead",
			ExecutedAt: time.Now().UTC(),
		},
		{
			ID:        uuid.New(),
			UserId:    userID,
			Query:     "queen",
			QueryNorm: "queen",
			ExecutedAt: time.Now().UTC().Add(-time.Hour),
		},
	}

	tests := []struct {
		name      string
		repo      *fakeSearchHistoryRepository
		nilRepo   bool
		limit     int
		wantCount int
		wantErr   bool
	}{
		{
			name: "happy path returns entries",
			repo: &fakeSearchHistoryRepository{
				listDistinctFn: func(_ context.Context, _ shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
					if limit != 5 {
						t.Errorf("expected limit 5, got %d", limit)
					}
					return fixedEntries, nil
				},
			},
			limit:     5,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "zero limit defaults to 10",
			repo: &fakeSearchHistoryRepository{
				listDistinctFn: func(_ context.Context, _ shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
					if limit != 10 {
						t.Errorf("expected default limit 10, got %d", limit)
					}
					return nil, nil
				},
			},
			limit:     0,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "negative limit defaults to 10",
			repo: &fakeSearchHistoryRepository{
				listDistinctFn: func(_ context.Context, _ shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
					if limit != 10 {
						t.Errorf("expected default limit 10 for negative input, got %d", limit)
					}
					return nil, nil
				},
			},
			limit:     -1,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "nil repo returns nil",
			nilRepo:   true,
			limit:     5,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "repo error propagates",
			repo: &fakeSearchHistoryRepository{
				listDistinctFn: func(_ context.Context, _ shared.UserId, _ int) ([]*domain.SearchHistoryEntry, error) {
					return nil, errors.New("db unavailable")
				},
			},
			limit:   5,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc *ListSearchHistoryService
			if tt.nilRepo {
				svc = NewListSearchHistoryService(nil)
			} else {
				svc = NewListSearchHistoryService(tt.repo)
			}

			entries, err := svc.Execute(context.Background(), userID, tt.limit)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if !tt.wantErr && len(entries) != tt.wantCount {
				t.Errorf("expected %d entries, got %d", tt.wantCount, len(entries))
			}
		})
	}
}
