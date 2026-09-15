package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestUserIDFromContext(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := shared.NewUserId(uuid.New())
		ctx := context.WithValue(context.Background(), userIDKey, uid)

		got, ok := UserIDFromContext(ctx)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
	})

	t.Run("absent", func(t *testing.T) {
		got, ok := UserIDFromContext(context.Background())
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
	})
}

func TestRequireUserID(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		uid := shared.NewUserId(uuid.New())
		ctx := context.WithValue(context.Background(), userIDKey, uid)
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		got, ok := RequireUserID(rec, req)
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		if got.UUID() != uid.UUID() {
			t.Errorf("userId: got %v, want %v", got.UUID(), uid.UUID())
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status: got %d, want %d (no error written)", rec.Code, http.StatusOK)
		}
	})

	t.Run("absent writes 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		got, ok := RequireUserID(rec, req)
		if ok {
			t.Fatal("expected ok=false, got true")
		}
		if !got.IsZero() {
			t.Errorf("expected zero UserId, got %v", got)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
