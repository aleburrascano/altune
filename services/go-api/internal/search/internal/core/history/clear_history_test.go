package history_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestClearSearchHistoryService_Execute(t *testing.T) {
	userID := ids.NewUserId(uuid.New())

	tests := []struct {
		name       string
		repo       *search.FakeHistoryEraser
		nilRepo    bool
		wantErr    bool
		wantCalled bool
	}{
		{
			name: "happy path deletes for user",
			repo: &search.FakeHistoryEraser{
				EraseFn: func(_ context.Context, gotUser ids.UserId) error {
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
			repo: &search.FakeHistoryEraser{
				EraseFn: func(_ context.Context, _ ids.UserId) error {
					return errors.New("db unavailable")
				},
			},
			wantErr:    true,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc *search.ClearSearchHistoryService
			var called bool
			if tt.nilRepo {
				svc = search.NewClearSearchHistoryService(nil)
			} else {
				inner := tt.repo.EraseFn
				tt.repo.EraseFn = func(ctx context.Context, u ids.UserId) error {
					called = true
					return inner(ctx, u)
				}
				svc = search.NewClearSearchHistoryService(tt.repo)
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
