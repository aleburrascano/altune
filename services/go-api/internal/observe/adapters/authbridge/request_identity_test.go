package authbridge

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequireUserID_NoUserWritesSame401AsMiddleware(t *testing.T) {
	want := httptest.NewRecorder()
	middleware.RequireUserID(want, httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ok || !got.IsZero() {
		t.Fatalf("got %v, %v; want zero, false", got, ok)
	}
	if rec.Code != http.StatusUnauthorized || rec.Code != want.Code {
		t.Fatalf("status = %d, want %d", rec.Code, want.Code)
	}
	if rec.Body.String() != want.Body.String() {
		t.Errorf("body = %q, want %q", rec.Body.String(), want.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") != want.Header().Get("WWW-Authenticate") {
		t.Errorf("WWW-Authenticate = %q, want %q", rec.Header().Get("WWW-Authenticate"), want.Header().Get("WWW-Authenticate"))
	}
}

func TestUserIDFromContext_ReturnsUserInContext(t *testing.T) {
	uid := shared.NewUserId(uuid.New())

	got, ok := UserIDFromContext(service.ContextWithUserID(context.Background(), uid))

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
}

func TestUserIDFromContext_NoUser(t *testing.T) {
	got, ok := UserIDFromContext(context.Background())

	if ok || !got.IsZero() {
		t.Fatalf("got %v, %v; want zero, false", got, ok)
	}
}

func TestUntilTokenExpiry_CauseIsErrTokenExpired(t *testing.T) {
	parent := service.ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if !errors.Is(context.Cause(ctx), domain.ErrTokenExpired) {
		t.Fatalf("cause = %v, want ErrTokenExpired", context.Cause(ctx))
	}
}
