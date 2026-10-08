package authbridge

import (
	"altune/go-api/internal/auth"
	authservice "altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequireUserID_NoUserMatchesAuthResponse(t *testing.T) {
	want := httptest.NewRecorder()
	auth.RequireUserID(want, httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ok || !got.IsZero() {
		t.Fatalf("got %v, %v; want zero, false", got, ok)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Body.String() != want.Body.String() || rec.Body.Len() == 0 {
		t.Errorf("body = %q, want %q", rec.Body.String(), want.Body.String())
	}
}

func TestRequireUserID_PresentWritesNothing(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(authservice.ContextWithUserID(context.Background(), uid))
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, req)

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("wrote body %q headers %v, want nothing", rec.Body.String(), rec.Header())
	}
}

func TestRequireUserID_RefusesLookAlikeKey(t *testing.T) {
	type otherKey struct{}
	uid := shared.NewUserId(uuid.New())
	ctx := context.WithValue(context.Background(), otherKey{}, uid.UUID().String())
	rec := httptest.NewRecorder()

	_, ok := RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))

	if ok || rec.Code != http.StatusUnauthorized {
		t.Fatalf("ok = %v, status = %d; want false, 401", ok, rec.Code)
	}
}

func TestUserIDFromContext(t *testing.T) {
	parent := context.Background()
	if _, ok := UserIDFromContext(parent); ok {
		t.Fatal("empty context returned a user")
	}
	uid := shared.NewUserId(uuid.New())

	got, ok := UserIDFromContext(authservice.ContextWithUserID(parent, uid))

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
}
