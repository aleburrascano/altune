package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	adminHandler "altune/go-api/internal/admin/handler"
	authMetrics "altune/go-api/internal/auth/adapters/metrics"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The production /admin tree counts its auth failures, and the operator reads
// those counts back from GET /admin/metrics/live on the same tree.
func TestMountAdmin_AuthRejectionsAndOutagesReachLiveMetrics(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (shared.UserId, error) {
		switch token {
		case operatorToken:
			return operator, nil
		case "jwks-down":
			return shared.UserId{}, errors.New("fetch JWKS: connection refused")
		}
		return shared.UserId{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	})
	r := chi.NewRouter()
	mountAdmin(r, verifier, adminPrincipals{operator: operator.String()}, adminHandler.New(nil, nil).WithLiveMetrics(liveMetricsSnapshot))

	before := authMetrics.ReadSnapshot()
	if code, _ := callAdmin(t, r, http.MethodGet, "/admin/metrics/live", "forged"); code != http.StatusUnauthorized {
		t.Fatalf("forged token: status %d, want 401", code)
	}
	if code, _ := callAdmin(t, r, http.MethodGet, "/admin/metrics/live", "jwks-down"); code != http.StatusServiceUnavailable {
		t.Fatalf("verifier down: status %d, want 503", code)
	}

	code, body := callAdmin(t, r, http.MethodGet, "/admin/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Auth authMetrics.Snapshot `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Auth.TokenRejections != before.TokenRejections+1 {
		t.Errorf("auth.token_rejections_total %d, want %d", got.Auth.TokenRejections, before.TokenRejections+1)
	}
	reason := string(auth.ReasonSignatureInvalid)
	if want := before.TokenRejectionsByReason[reason] + 1; got.Auth.TokenRejectionsByReason[reason] != want {
		t.Errorf("auth.token_rejections_by_reason_total[%s] %d, want %d", reason, got.Auth.TokenRejectionsByReason[reason], want)
	}
	if got.Auth.VerifierUnavailable != before.VerifierUnavailable+1 {
		t.Errorf("auth.verifier_unavailable_total %d, want %d", got.Auth.VerifierUnavailable, before.VerifierUnavailable+1)
	}
}
