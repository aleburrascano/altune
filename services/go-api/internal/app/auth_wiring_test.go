package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/config"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type jwksFixture struct {
	key      *rsa.PrivateKey
	operator uuid.UUID
	cfg      *config.Config
	jwks     *httptest.Server
	keysDown *atomic.Bool
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func newJWKSFixture(t *testing.T) jwksFixture {
	t.Helper()
	key := newRSAKey(t)
	pub, err := jwk.FromRaw(key.PublicKey)
	if err != nil {
		t.Fatalf("jwk from public key: %v", err)
	}
	_ = pub.Set(jwk.KeyIDKey, "k1")
	_ = pub.Set(jwk.AlgorithmKey, jwa.RS256)
	set := jwk.NewSet()
	_ = set.AddKey(pub)
	keysDown := &atomic.Bool{}
	setJSON, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("encode the key set: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if keysDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(setJSON)
	}))
	t.Cleanup(srv.Close)
	operator := uuid.New()
	return jwksFixture{key: key, operator: operator, jwks: srv, keysDown: keysDown, cfg: &config.Config{
		MusicDir:                       t.TempDir(),
		AcquisitionDownloadConcurrency: 1,
		AcquisitionUpgradeConcurrency:  1,
		SupabaseJWTJWKSURL:             srv.URL,
		SupabaseProjectURL:             "https://p.supabase.co",
		SupabaseJWTAud:                 "authenticated",
		OverseerPrincipalID:            operator.String(),
		BackgroundMaxInFlight:          64,

		AcquisitionFileHostPageTimeout:  10 * time.Second,
		AcquisitionDeezerPreviewTimeout: 20 * time.Second,

		PlaybackNowPlayingLookupTimeout: 3 * time.Second,
		PlaybackErasureScanTimeout:      30 * time.Second,
	}}
}

func jwksConfig(t *testing.T) *config.Config {
	t.Helper()
	return newJWKSFixture(t).cfg
}

func signedToken(t *testing.T, key *rsa.PrivateKey, userID uuid.UUID) string {
	t.Helper()
	return signedTokenWithKeyID(t, key, "k1", userID)
}

func signedTokenWithKeyID(t *testing.T, key *rsa.PrivateKey, keyID string, userID uuid.UUID) string {
	t.Helper()
	tok := jwt.New()
	_ = tok.Set("sub", userID.String())
	_ = tok.Set("iss", "https://p.supabase.co/auth/v1")
	_ = tok.Set("aud", "authenticated")
	_ = tok.Set("iat", time.Now().Add(-time.Minute))
	_ = tok.Set("exp", time.Now().Add(30*time.Minute))
	priv, err := jwk.FromRaw(key)
	if err != nil {
		t.Fatalf("jwk from private key: %v", err)
	}
	_ = priv.Set(jwk.KeyIDKey, keyID)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, priv))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return string(signed)
}

func assembledHandler(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown(context.Background())
	})
	return assembly.Handler()
}

func getAs(t *testing.T, h http.Handler, path, token string) (int, []byte) {
	t.Helper()
	return getFrom(t, h, "192.0.2.1:1234", path, token)
}

func getFrom(t *testing.T, h http.Handler, client, path, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = client
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, body
}

func liveAuthMetrics(t *testing.T, h http.Handler, token string) auth.MetricsSnapshot {
	t.Helper()
	code, body := getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		Auth auth.MetricsSnapshot `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Auth
}

func TestAuth_APlaintextJWKSURLFailsAssembly(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.SupabaseJWTJWKSURL = "http://jwks.example.com/keys"

	_, err := app.Assemble(t.Context(), cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})

	if err == nil || !strings.HasPrefix(err.Error(), "auth: ") {
		t.Fatalf("Assemble error = %v, want the auth-wrapped refusal of a plaintext JWKS URL", err)
	}
}

func TestAuth_TheConfiguredVerifierAdmitsTheOperatorAndIsProbedForHealth(t *testing.T) {
	f := newJWKSFixture(t)
	h := assembledHandler(t, f.cfg)

	code, body := getAs(t, h, "/observe/health", signedToken(t, f.key, f.operator))

	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		t.Fatalf("GET /observe/health with the operator's token = %d, want the operator admitted; body %s", code, body)
	}
	var got struct {
		Auth string `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Auth != string(monitoring.DepUp) {
		t.Errorf("auth dependency = %q, want %q: the verifier the routes use must also be probed for health", got.Auth, monitoring.DepUp)
	}
}

func TestAuth_RejectionsAndAnUnreachableKeySetReachLiveMetrics(t *testing.T) {
	f := newJWKSFixture(t)
	f.keysDown.Store(true)
	h := assembledHandler(t, f.cfg)
	operator := signedToken(t, f.key, f.operator)
	before := auth.ReadSnapshot()

	const firstClient, forgingClient = "198.51.100.7:1234", "198.51.100.8:1234"
	if code, body := getFrom(t, h, firstClient, "/observe/metrics/live", operator); code != http.StatusServiceUnavailable {
		t.Fatalf("a token while the key set cannot be fetched: status %d, want 503; body %s", code, body)
	}
	unavailable := int64(1)
	f.keysDown.Store(false)
	for deadline := time.Now().Add(10 * time.Second); ; {
		code, body := getFrom(t, h, fmt.Sprintf("203.0.113.%d:1234", unavailable), "/observe/metrics/live", operator)
		if code == http.StatusOK {
			break
		}
		if code != http.StatusServiceUnavailable || time.Now().After(deadline) {
			t.Fatalf("the operator once the key set is back: status %d, want 200 within 10s; body %s", code, body)
		}
		unavailable++
		time.Sleep(100 * time.Millisecond)
	}
	if code, _ := getFrom(t, h, forgingClient, "/observe/metrics/live", signedToken(t, newRSAKey(t), f.operator)); code != http.StatusUnauthorized {
		t.Fatalf("token signed by another key: status %d, want 401", code)
	}

	after := liveAuthMetrics(t, h, operator)
	if after.TokenRejections != before.TokenRejections+1 {
		t.Errorf("auth.token_rejections_total %d, want %d", after.TokenRejections, before.TokenRejections+1)
	}
	reason := string(auth.ReasonSignatureInvalid)
	if want := before.TokenRejectionsByReason[reason] + 1; after.TokenRejectionsByReason[reason] != want {
		t.Errorf("auth.token_rejections_by_reason_total[%s] %d, want %d", reason, after.TokenRejectionsByReason[reason], want)
	}
	if after.VerifierUnavailable != before.VerifierUnavailable+unavailable {
		t.Errorf("auth.verifier_unavailable_total %d, want %d, one per 503 served", after.VerifierUnavailable, before.VerifierUnavailable+unavailable)
	}
}
