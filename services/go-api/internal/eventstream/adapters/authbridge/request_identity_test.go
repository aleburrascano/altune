package authbridge

import (
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

func TestRequireUserID_NoUserWritesPinned401(t *testing.T) {
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ok || !got.IsZero() {
		t.Fatalf("got %v, %v; want zero, false", got, ok)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	want := `{"detail":"authentication required","reason":"missing"}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestRequireUserID_PresentWritesNothing(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(service.ContextWithUserID(context.Background(), uid))
	rec := httptest.NewRecorder()

	got, ok := RequireUserID(rec, req)

	if !ok || got.UUID() != uid.UUID() {
		t.Fatalf("got %v, %v; want %v, true", got.UUID(), ok, uid.UUID())
	}
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("wrote body %q headers %v, want nothing", rec.Body.String(), rec.Header())
	}
}

func TestUntilTokenExpiry_CauseMatchesErrTokenExpired(t *testing.T) {
	parent := service.ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if !errors.Is(context.Cause(ctx), ErrTokenExpired) {
		t.Fatalf("cause = %v, want ErrTokenExpired", context.Cause(ctx))
	}
}
