package auth_test

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type wireRejection struct {
	Status          int             `json:"status"`
	WWWAuthenticate string          `json:"www_authenticate"`
	Body            json.RawMessage `json:"body"`
}

func rejectionWire(t *testing.T, verifierErr error) []byte {
	t.Helper()
	next, _ := noopHandler()
	handler := auth.MustMiddleware(stubVerifier(ids.UserId{}, verifierErr), time.Now, silentAuthMetrics{}, auth.DefaultFailureLimits)(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks", nil)
	req.Header.Set("Authorization", "Bearer contract-fixture-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	out, err := json.Marshal(wireRejection{
		Status:          rec.Code,
		WWWAuthenticate: rec.Header().Get("WWW-Authenticate"),
		Body:            rec.Body.Bytes(),
	})
	if err != nil {
		t.Fatalf("marshal rejection: %v", err)
	}
	return out
}

func TestWire_ARejectedTokenAnswersTheStatusTheClientTreatsAsSessionExpired(t *testing.T) {
	out := rejectionWire(t, &auth.InvalidTokenError{Reason: reasonExpired, Detail: "token expired"})

	jsoncontract.AssertWireExact(t, "auth_token_rejected", out)
}

func TestWire_AnUnavailableVerifierAnswersAStatusThatDoesNotExpireTheSession(t *testing.T) {
	out := rejectionWire(t, errors.New("jwks fetch failed"))

	jsoncontract.AssertWireExact(t, "auth_verifier_unavailable", out)
}
