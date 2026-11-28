package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
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
		repo       *memory.FakeHistoryEraser
		wantErr    bool
		wantCalled bool
	}{
		{
			name: "happy path deletes for user",
			repo: &memory.FakeHistoryEraser{
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
			name: "repo error propagates",
			repo: &memory.FakeHistoryEraser{
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
			var called bool
			inner := tt.repo.EraseFn
			tt.repo.EraseFn = func(ctx context.Context, u ids.UserId) error {
				called = true
				return inner(ctx, u)
			}
			svc := searchtest.NewClearHistory(t, tt.repo)

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
	svc, err := service.NewClearSearchHistoryService(&memory.FakeHistoryEraser{}, nil)

	if !errors.Is(err, domain.ErrHistoryClockRequired) || svc != nil {
		t.Fatalf("NewClearSearchHistoryService(nil clock) = (%v, %v), want history.ErrClockRequired and no service", svc, err)
	}
}

func TestNewClearSearchHistoryService_RefusesAMissingRepository(t *testing.T) {
	svc, err := service.NewClearSearchHistoryService(nil, time.Now)

	if svc != nil || !errors.Is(err, domain.ErrHistoryCollaboratorRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping history.ErrCollaboratorRequired", svc, err)
	}
}

func TestClearSearchHistory_AuditLineCarriesTheInjectedTime(t *testing.T) {
	at := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	ring := memory.CaptureDetachedLogs(t)
	svc := service.MustNewClearSearchHistoryService(&memory.FakeHistoryEraser{
		EraseFn: func(context.Context, ids.UserId) error { return nil },
	}, clocktest.NewManual(at).Func())

	if err := svc.Execute(context.Background(), memory.NewUser()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := memory.OnlyRecord(t, ring, "discovery.search_history_cleared")
	if rec.Attrs["at"] != "2026-07-08 09:10:11 +0000 UTC" {
		t.Errorf("audit line at = %q, want the injected time in UTC", rec.Attrs["at"])
	}
}
