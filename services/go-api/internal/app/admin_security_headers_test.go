package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func mountedAdminTree(t *testing.T) *chi.Mux {
	t.Helper()
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil).
		WithSupabaseLogin("https://proj.supabase.co", "anon-key"))
	return r
}

func headersOf(t *testing.T, srv http.Handler, method, path string) http.Header {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Header()
}

// Every route under /admin answers an authenticated operator, so none of them
// may be stored by a shared cache or content-sniffed. Walking the tree is what
// makes a route added later inherit the rule instead of opting into it (#1995).
func TestMountAdmin_EveryRouteSendsNoStoreAndNosniff(t *testing.T) {
	tree := mountedAdminTree(t)

	walked := 0
	err := chi.Walk(tree, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked++
		headers := headersOf(t, tree, method, route)
		if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s %s: X-Content-Type-Options = %q, want nosniff", method, route, got)
		}
		if got := headers.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", method, route, got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk admin routes: %v", err)
	}
	if walked == 0 {
		t.Fatal("walked no admin routes, so the assertions proved nothing")
	}
}

// The index is the document the token-holding script runs in, so it carries the
// framing and policy headers on top of the tree-wide pair.
func TestMountAdmin_IndexSendsItsDocumentHeaders(t *testing.T) {
	headers := headersOf(t, mountedAdminTree(t), http.MethodGet, "/admin/")

	if got := headers.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := headers.Get("Content-Security-Policy"); got == "" {
		t.Error("Content-Security-Policy is empty on the admin index")
	}
}
