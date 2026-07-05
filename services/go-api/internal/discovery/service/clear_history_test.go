package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestClearSearchHistoryService_Execute(t *testing.T) {
	userID := shared.NewUserId(uuid.New())

	tests := []struct {
		name       string
		repo       *fakeSearchHistoryRepository
		nilRepo    bool
		wantErr    bool
		wantCalled bool
	}{
		{
			name: "happy path deletes for user",
			repo: &fakeSearchHistoryRepository{
				deleteAllFn: func(_ context.Context, gotUser shared.UserId) error {
					if gotUser != userID {
						t.Errorf("expected user %v, got %v", userID, gotUser)
					}
					return nil
				},
			},
			wantErr:    false,
			wantCalled: true,
		},
		{
			name:    "nil repo is a no-op",
			nilRepo: true,
			wantErr: false,
		},
		{
			name: "repo error propagates",
			repo: &fakeSearchHistoryRepository{
				deleteAllFn: func(_ context.Context, _ shared.UserId) error {
					return errors.New("db unavailable")
				},
			},
			wantErr:    true,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc *ClearSearchHistoryService
			var called bool
			if tt.nilRepo {
				svc = NewClearSearchHistoryService(nil)
			} else {
				inner := tt.repo.deleteAllFn
				tt.repo.deleteAllFn = func(ctx context.Context, u shared.UserId) error {
					called = true
					return inner(ctx, u)
				}
				svc = NewClearSearchHistoryService(tt.repo)
			}

			err := svc.Execute(context.Background(), userID)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if called != tt.wantCalled {
				t.Errorf("expected repo called=%v, got %v", tt.wantCalled, called)
			}
		})
	}
}
