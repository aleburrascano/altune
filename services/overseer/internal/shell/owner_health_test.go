package shell_test

import (
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/shell"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	healthSeedRefresh = "seed-refresh-token-SECRET-health"
	healthAnonKey     = "anon-key-SECRET-health"
	healthPassword    = "readonly-PASSWORD-SECRET-health"
)

type ownerHealthBody struct {
	LastCycle     string `json:"lastCycle"`
	BucketsOK     int    `json:"bucketsOk"`
	BucketsFailed int    `json:"bucketsFailed"`
	Credential    *struct {
		OK                  bool   `json:"ok"`
		LastRefresh         string `json:"lastRefresh"`
		ConsecutiveFailures int    `json:"consecutiveFailures"`
		PersistFailed       bool   `json:"persistFailed"`
		PasswordGrant       bool   `json:"passwordGrant"`
		LastError           string `json:"lastError"`
	} `json:"credential"`
}

func rejectingTokenSource(t *testing.T) *goapi.RefreshingTokenSource {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"refresh_token_already_used"}`)
	}))
	t.Cleanup(endpoint.Close)
	src, err := goapi.NewRefreshingTokenSource(endpoint.URL, healthAnonKey, healthSeedRefresh,
		goapi.WithRefreshHTTPClient(endpoint.Client()),
		goapi.WithPasswordGrant("readonly@altune.test", healthPassword))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src
}

func getOwnerHealth(t *testing.T, srv http.Handler) (int, string, ownerHealthBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/health", nil)))
	var body ownerHealthBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("owner health unmarshal: %v", err)
		}
	}
	return rec.Code, rec.Body.String(), body
}

func TestOwnerHealthRejectsMissingBearer(t *testing.T) {
	srv := newServer(fixedRegistry{})

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/health without bearer = %d, want 401", rec.Code)
	}
}

func TestOwnerHealthForbidsNonOwner(t *testing.T) {
	srv := newServer(fixedRegistry{})
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+nonOwnerToken)

	rec := do(srv, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/health with non-owner token = %d, want 403", rec.Code)
	}
}

func TestOwnerHealthShowsAFailingCredentialWithoutItsSecrets(t *testing.T) {
	tokens := rejectingTokenSource(t)
	_, _ = tokens.Token(context.Background())
	srv := newServer(fixedRegistry{}, shell.WithCredentialHealth(tokens.Health))

	code, raw, body := getOwnerHealth(t, srv)

	if code != http.StatusOK {
		t.Fatalf("owner GET /api/health = %d, want 200", code)
	}
	if body.Credential == nil {
		t.Fatalf("owner health %s carries no credential", raw)
	}
	if body.Credential.OK || body.Credential.ConsecutiveFailures < 1 {
		t.Errorf("credential = %+v, want ok=false with the failure count", *body.Credential)
	}
	if body.Credential.LastError == "" || !body.Credential.PasswordGrant {
		t.Errorf("credential = %+v, want the failure class and the configured password grant", *body.Credential)
	}
	for _, secret := range []string{healthSeedRefresh, healthAnonKey, healthPassword, "readonly@altune.test", "refresh_token_already_used"} {
		if strings.Contains(raw, secret) {
			t.Errorf("owner health %s leaks %q", raw, secret)
		}
	}
}

func TestOwnerHealthReportsTheLastCollectCycle(t *testing.T) {
	lastCycle := time.Date(2026, 9, 18, 10, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	srv := newServer(fixedRegistry{}, fixedCollectStatus(shell.CollectStatus{
		Healthy: true, LastCycle: lastCycle, OK: 6, Failed: 4,
	}))

	_, raw, body := getOwnerHealth(t, srv)

	if body.LastCycle != "2026-09-18T08:00:00Z" || body.BucketsOK != 6 || body.BucketsFailed != 4 {
		t.Errorf("owner health = %s, want the last cycle in UTC and its counts", raw)
	}
}

func TestOwnerHealthOmitsCredentialWhenNoRefreshingSourceIsWired(t *testing.T) {
	srv := newServer(fixedRegistry{})

	_, raw, body := getOwnerHealth(t, srv)

	if body.Credential != nil || strings.Contains(raw, "credential") {
		t.Errorf("owner health %s reports a credential nobody wired", raw)
	}
}

func TestPublicHealthStaysMinimalWithACredentialWired(t *testing.T) {
	tokens := rejectingTokenSource(t)
	_, _ = tokens.Token(context.Background())
	srv := newServer(fixedRegistry{},
		shell.WithCredentialHealth(tokens.Health),
		fixedCollectStatus(shell.CollectStatus{Healthy: true, LastCycle: time.Now(), OK: 1, Failed: 2}))

	rec := do(srv, httptest.NewRequest(http.MethodGet, "/health", nil))

	var fields map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("health unmarshal: %v", err)
	}
	for _, key := range []string{"status", "last_cycle", "buckets_ok", "buckets_failed"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("public /health %s lost %q", rec.Body.String(), key)
		}
	}
	if len(fields) != 4 {
		t.Errorf("public /health %s, want exactly status, last_cycle, buckets_ok, buckets_failed", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Errorf("public /health = %d with a failing credential, want 200", rec.Code)
	}
}
