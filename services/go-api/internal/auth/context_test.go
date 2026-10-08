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

func TestRequireUserID_AbsentBodyIsPinned(t *testing.T) {
	rec := httptest.NewRecorder()

	_, ok := RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ok {
		t.Fatal("expected ok=false, got true")
	}
	requireExactResponse(t, rec, http.StatusUnauthorized, "Bearer", `{"detail":"authentication required","reason":"missing"}`+"\n")
}

func TestRequireUserID_PresentWritesNothing(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ContextWithUserID(context.Background(), uid))
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, req)

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("wrote body %q headers %v, want nothing", rec.Body.String(), rec.Header())
	}
}

func TestRequireUserID_ChangesBetweenTicks(t *testing.T) {
	base := httptest.NewRequest(http.MethodGet, "/", nil)

	first := httptest.NewRecorder()
	if _, ok := RequireUserID(first, base); ok {
		t.Fatal("first tick: expected ok=false without a user")
	}
	requireExactResponse(t, first, http.StatusUnauthorized, "Bearer", `{"detail":"authentication required","reason":"missing"}`+"\n")

	uid := shared.NewUserId(uuid.New())
	second := httptest.NewRecorder()
	got, ok := RequireUserID(second, base.WithContext(ContextWithUserID(base.Context(), uid)))
	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("second tick: got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if second.Body.Len() != 0 {
		t.Errorf("second tick wrote body %q, want none", second.Body.String())
	}
}
