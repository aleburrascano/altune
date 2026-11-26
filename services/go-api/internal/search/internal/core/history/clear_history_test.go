package history_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

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
				svc = search.NewClearSearchHistoryService(nil, time.Now)
			} else {
				inner := tt.repo.EraseFn
				tt.repo.EraseFn = func(ctx context.Context, u ids.UserId) error {
					called = true
					return inner(ctx, u)
				}
				svc = search.NewClearSearchHistoryService(tt.repo, time.Now)
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

func TestNewClearSearchHistoryService_RefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a nil clock was accepted, want a panic at construction")
		}
	}()

	search.NewClearSearchHistoryService(&search.FakeHistoryEraser{}, nil)
}

func TestClearSearchHistory_AuditLineCarriesTheInjectedTime(t *testing.T) {
	at := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	ring := search.CaptureDetachedLogs(t)
	svc := search.NewClearSearchHistoryService(&search.FakeHistoryEraser{
		EraseFn: func(context.Context, ids.UserId) error { return nil },
	}, clocktest.NewManual(at).Func())

	if err := svc.Execute(context.Background(), search.NewUser()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := search.OnlyRecord(t, ring, "discovery.search_history_cleared")
	if rec.Attrs["at"] != "2026-07-08 09:10:11 +0000 UTC" {
		t.Errorf("audit line at = %q, want the injected time in UTC", rec.Attrs["at"])
	}
}
