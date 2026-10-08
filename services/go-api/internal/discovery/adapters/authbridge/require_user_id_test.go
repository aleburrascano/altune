package authbridge

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func newUserID() shared.UserId { return shared.NewUserId(uuid.New()) }

func TestRequireUserID_NoUserMatchesRootResponse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	wantRec := httptest.NewRecorder()
	if _, ok := auth.RequireUserID(wantRec, req); ok {
		t.Fatal("root RequireUserID accepted a request without a user")
	}
	gotRec := httptest.NewRecorder()
	if _, ok := RequireUserID(gotRec, req); ok {
		t.Fatal("bridge RequireUserID accepted a request without a user")
	}

	if gotRec.Code != http.StatusUnauthorized || gotRec.Code != wantRec.Code {
		t.Fatalf("status = %d, want %d (401)", gotRec.Code, wantRec.Code)
	}
	if gotRec.Body.String() != wantRec.Body.String() {
		t.Fatalf("body = %q, want %q", gotRec.Body.String(), wantRec.Body.String())
	}
}

func TestRequireUserID_UserInContext(t *testing.T) {
	id := newUserID()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(service.ContextWithUserID(req.Context(), id))

	rec := httptest.NewRecorder()
	got, ok := RequireUserID(rec, req)
	if !ok || got != id {
		t.Fatalf("got (%v, %v), want (%v, true)", got, ok, id)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("unexpected body %q", rec.Body.String())
	}
}

type foreignKey struct{}

func TestUserIDFromContext_RefusesForeignKey(t *testing.T) {
	ctx := context.WithValue(context.Background(), foreignKey{}, newUserID())
	if _, ok := UserIDFromContext(ctx); ok {
		t.Fatal("user id stored under a foreign key was accepted")
	}
}

func TestUserIDFromContext_SequentialRequestsKeepOwnUser(t *testing.T) {
	first, second := newUserID(), newUserID()
	for _, want := range []shared.UserId{first, second} {
		ctx := service.ContextWithUserID(context.Background(), want)
		got, ok := UserIDFromContext(ctx)
		if !ok || got != want {
			t.Fatalf("got (%v, %v), want (%v, true)", got, ok, want)
		}
	}
}

func TestClientKey_MatchesRoot(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.5:4242"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got, want := ClientKey(req), auth.ClientKey(req); got != want {
		t.Fatalf("ClientKey = %q, want %q", got, want)
	}
}
