package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// --- stub TokenVerifier ---

func stubVerifier(userID shared.UserId, err error) VerifierFunc {
	return func(context.Context, string) (shared.UserId, error) {
		return userID, err
	}
}

// --- helpers ---

// noopHandler records whether it was called.
func noopHandler() (http.Handler, *bool) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &called
}

func decodeRejectBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode reject body: %v", err)
	}
	return body
}

// --- Middleware tests ---

func TestMiddleware_MissingHeader(t *testing.T) {
	next, called := noopHandler()
	handler := Middleware(stubVerifier(shared.UserId{}, nil))(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != string(ReasonMissing) {
		t.Errorf("reason: got %q, want %q", body["reason"], ReasonMissing)
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_MalformedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "Basic scheme", header: "Basic abc123"},
		{name: "no space separator", header: "Bearertoken"},
		{name: "empty scheme", header: " token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, called := noopHandler()
			handler := Middleware(stubVerifier(shared.UserId{}, nil))(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			body := decodeRejectBody(t, rec)
			if body["reason"] != string(ReasonMalformed) {
				t.Errorf("reason: got %q, want %q", body["reason"], ReasonMalformed)
			}
			if *called {
				t.Error("next handler should not have been called")
			}
		})
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	verifier := stubVerifier(uid, nil)

	var capturedUID shared.UserId
	var uidFound bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID, uidFound = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(verifier)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !uidFound {
		t.Fatal("expected userId in context, got none")
	}
	if capturedUID.UUID() != uid.UUID() {
		t.Errorf("context userId: got %v, want %v", capturedUID.UUID(), uid.UUID())
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	verifier := stubVerifier(shared.UserId{},
		&InvalidTokenError{Reason: ReasonExpired, Detail: "token expired"})
	next, called := noopHandler()
	handler := Middleware(verifier)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != string(ReasonExpired) {
		t.Errorf("reason: got %q, want %q", body["reason"], ReasonExpired)
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

func TestMiddleware_VerifierError(t *testing.T) {
	// A non-InvalidTokenError means the verifier could not run (e.g. JWKS
	// unreachable) — infrastructure failure, not a bad token: 503, not 401.
	verifier := stubVerifier(shared.UserId{}, errors.New("fetch JWKS: connection refused"))
	next, called := noopHandler()
	handler := Middleware(verifier)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	body := decodeRejectBody(t, rec)
	if body["reason"] != "" {
		t.Errorf("reason: got %q, want none (infra failure is not a token rejection)", body["reason"])
	}
	if *called {
		t.Error("next handler should not have been called")
	}
}

// --- context helper tests ---

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
		// Should not have written an error response
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
