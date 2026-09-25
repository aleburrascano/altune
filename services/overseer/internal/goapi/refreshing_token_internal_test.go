package goapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The refresh token the tests seed and the access tokens the stub mints. The
// secret-leakage assertions grep for these exact strings.
const (
	rtsSeedRefresh = "seed-refresh-token-SECRET-000"
	rtsAccessMark  = "ACCESS-TOKEN-SECRET"
)

// rtsFakeClock is a race-safe injectable clock so the proactive-refresh math is
// driven deterministically from concurrent goroutines.
type rtsFakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *rtsFakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *rtsFakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// rtsMakeJWT builds an unsigned-looking JWT whose exp claim is expUnix. Only the
// exp claim matters: the source parses it without verifying the signature.
func rtsMakeJWT(mark string, expUnix int64) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"mark":%q}`, expUnix, mark)))
	return hdr + "." + payload + ".sig-" + mark
}

// rtsStub is a fake Supabase token endpoint. It records how many exchanges it
// served and the refresh token each one presented, and mints a fresh access token
// per call so the proactive-refresh path is observable.
type rtsStub struct {
	clock     *rtsFakeClock
	lifetime  time.Duration
	calls     atomic.Int64
	release   chan struct{} // when non-nil, each handler blocks until closed
	mu        sync.Mutex
	gotAPIKey []string
	gotBody   []refreshGrantBody
	// overrides for hostile-response tests
	status  int
	rawBody string
	rotate  bool // emit a rotated refresh_token
}

func (s *rtsStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.release != nil {
		<-s.release
	}
	n := s.calls.Add(1)

	var body refreshGrantBody
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	s.mu.Lock()
	s.gotAPIKey = append(s.gotAPIKey, r.Header.Get("apikey"))
	s.gotBody = append(s.gotBody, body)
	s.mu.Unlock()

	if s.status != 0 {
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.rawBody)
		return
	}
	if s.rawBody != "" {
		_, _ = io.WriteString(w, s.rawBody)
		return
	}

	exp := s.clock.now().Add(s.lifetime).Unix()
	resp := map[string]any{
		"access_token": rtsMakeJWT(fmt.Sprintf("%s-%d", rtsAccessMark, n), exp),
		"token_type":   "bearer",
		"expires_in":   int(s.lifetime.Seconds()),
	}
	if s.rotate {
		resp["refresh_token"] = fmt.Sprintf("rotated-refresh-%d", n)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// rtsNewSource wires a RefreshingTokenSource at the stub with the fake clock.
func rtsNewSource(t *testing.T, stub *rtsStub) (*RefreshingTokenSource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(stub.clock.now))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()
	src.http.Timeout = 5 * time.Second
	return src, srv
}

func rtsClock() *rtsFakeClock {
	return &rtsFakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestRefreshingExchangesRefreshForAccess(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if !strings.Contains(tok, rtsAccessMark) {
		t.Fatalf("token %q does not look like the minted access token", tok)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want 1", got)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotAPIKey[0] != "anon-key-SECRET" {
		t.Fatalf("apikey header = %q, want anon-key-SECRET", stub.gotAPIKey[0])
	}
	if stub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("refresh_token body = %q, want seed", stub.gotBody[0].RefreshToken)
	}
}

func TestRefreshingCachesWithinWindow(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)

	first, _ := src.Token(context.Background())
	clock.advance(30 * time.Minute) // inside the 48-minute (80%) window
	second, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if first != second {
		t.Fatal("expected the cached token to be reused inside the proactive window")
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want 1 (served from cache)", got)
	}
}

func TestRefreshingProactiveRefreshBeforeExpiry(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)

	first, _ := src.Token(context.Background())
	// 80% of a 1h token is 48m. Just before: still cached.
	clock.advance(47 * time.Minute)
	again, _ := src.Token(context.Background())
	if again != first || stub.calls.Load() != 1 {
		t.Fatalf("token refreshed too early: calls=%d", stub.calls.Load())
	}
	// Cross the 80% mark while the token is still valid (exp is at 60m): refresh.
	clock.advance(2 * time.Minute) // now at 49m
	refreshed, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if refreshed == first {
		t.Fatal("expected a proactive refresh past 80% of lifetime")
	}
	if stub.calls.Load() != 2 {
		t.Fatalf("exchanges = %d, want 2", stub.calls.Load())
	}
}

func TestRefreshingSingleFlightUnderConcurrency(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, release: make(chan struct{})}
	src, _ := rtsNewSource(t, stub)

	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	tokens := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tokens[i], errs[i] = src.Token(context.Background())
		}(i)
	}
	close(start)
	// Give every goroutine time to pile onto the single in-flight exchange, then
	// release the one blocked handler.
	time.Sleep(50 * time.Millisecond)
	close(stub.release)
	wg.Wait()

	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("network exchanges = %d, want exactly 1 (single-flight)", got)
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if tokens[i] != tokens[0] {
			t.Fatalf("goroutine %d got a different token; single-flight should share one", i)
		}
	}
}

func TestRefreshingRefreshOnInvalidate(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)

	first, _ := src.Token(context.Background())
	src.invalidateRejected(first) // models the client dropping the 401'd token before its window
	second, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after invalidate: %v", err)
	}
	if second == first {
		t.Fatal("expected a fresh exchange after invalidate")
	}
	if stub.calls.Load() != 2 {
		t.Fatalf("exchanges = %d, want 2", stub.calls.Load())
	}
}

// TestRefreshingRefreshOn401ViaClient exercises the 401 retry through
// AdminHealth: Health is now public and unauthenticated by design (#2357), so
// it can never 401, but AdminHealth is still bearer-guarded and is the read
// this retry path exists for.
func TestRefreshingRefreshOn401ViaClient(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, tokenSrv := rtsNewSource(t, stub)

	var apiCalls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if apiCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
	}))
	t.Cleanup(api.Close)
	_ = tokenSrv

	client, err := New(api.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	health, err := client.AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth after 401 retry: %v", err)
	}
	if !health.Healthy() {
		t.Fatalf("health = %+v, want healthy", health)
	}
	if apiCalls.Load() != 2 {
		t.Fatalf("go-api calls = %d, want 2 (401 then retry)", apiCalls.Load())
	}
	if stub.calls.Load() != 2 {
		t.Fatalf("token exchanges = %d, want 2 (initial + after 401 invalidate)", stub.calls.Load())
	}
}

func TestRefreshingStatusFailureTypedError(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, status: http.StatusInternalServerError, rawBody: "boom"}
	src, _ := rtsNewSource(t, stub)

	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("want a refresh error on a 500 from the token endpoint")
	}
	var tre *TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *TokenRefreshError", err)
	}
	if tre.Status != http.StatusInternalServerError {
		t.Fatalf("Status = %d, want 500", tre.Status)
	}
}

func TestRefreshingRejectsExpiredAndBadExp(t *testing.T) {
	expiredUnix := rtsClock().now().Add(-time.Minute).Unix()
	cases := map[string]string{
		"expired":   `{"access_token":"` + rtsMakeJWT("x", expiredUnix) + `"}`,
		"no-exp":    `{"access_token":"` + rtsMakeNoExpJWT() + `"}`,
		"not-a-jwt": `{"access_token":"not-a-jwt"}`,
		"missing":   `{"token_type":"bearer"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			clock := rtsClock()
			stub := &rtsStub{clock: clock, lifetime: time.Hour, rawBody: raw}
			src, _ := rtsNewSource(t, stub)
			if _, err := src.Token(context.Background()); err == nil {
				t.Fatalf("want a typed error for %s response", name)
			} else {
				var tre *TokenRefreshError
				if !errors.As(err, &tre) {
					t.Fatalf("error %v is not a *TokenRefreshError", err)
				}
			}
		})
	}
}

func rtsMakeNoExpJWT() string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`))
	return hdr + "." + payload + ".sig"
}

func TestRefreshingHostileHugeBodyIsBounded(t *testing.T) {
	// A multi-megabyte body must neither hang nor be read past the cap: it is
	// invalid JSON once truncated, so the exchange fails typed instead of OOMing.
	huge := `{"access_token":"` + strings.Repeat("A", 4<<20) + `"`
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rawBody: huge}
	src, _ := rtsNewSource(t, stub)
	done := make(chan error, 1)
	go func() { _, e := src.Token(context.Background()); done <- e }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want an error on a truncated huge body")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Token hung on a huge body")
	}
}

func TestRefreshingHugeExpDoesNotOverflow(t *testing.T) {
	// A hostile exp absurdly far in the future must not overflow the 4/5 multiply
	// nor pin the cache for millennia: the lifetime is capped, so the token is
	// accepted with a bounded refresh window and nothing panics.
	clock := rtsClock()
	huge := `{"access_token":"` + rtsMakeJWT("huge", 1<<62) + `"}`
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rawBody: huge}
	src, _ := rtsNewSource(t, stub)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("huge exp should be accepted with a capped window, got %v", err)
	}
	src.mu.Lock()
	window := src.refreshAt.Sub(clock.now())
	src.mu.Unlock()
	if window > maxAcceptedLifetime {
		t.Fatalf("refresh window %s exceeds the cap %s", window, maxAcceptedLifetime)
	}
}

func TestRefreshingRotationPersistsAndNeverBricks(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: true}
	src, _ := rtsNewSource(t, stub)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("first Token: %v", err)
	}
	clock.advance(50 * time.Minute) // force a proactive refresh
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("first exchange used %q, want seed", stub.gotBody[0].RefreshToken)
	}
	if stub.gotBody[1].RefreshToken != "rotated-refresh-1" {
		t.Fatalf("second exchange used %q, want the rotated token", stub.gotBody[1].RefreshToken)
	}
}

func TestRefreshingOmittedRotationKeepsSeed(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: false} // response omits refresh_token
	src, _ := rtsNewSource(t, stub)

	_, _ = src.Token(context.Background())
	clock.advance(50 * time.Minute)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotBody[1].RefreshToken != rtsSeedRefresh {
		t.Fatalf("second exchange used %q; an omitted rotation must keep the seed, not blank it", stub.gotBody[1].RefreshToken)
	}
}

func TestRefreshingNoSecretsInRenderOrLogs(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	tok, _ := src.Token(context.Background())

	// The rendered/formatted forms of the source must not carry token material.
	renders := []string{
		src.String(),
		fmt.Sprintf("%v", src),
		fmt.Sprintf("%+v", src),
		fmt.Sprintf("wired source: %s", src),
	}
	// slog output of the source.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("wired", "source", src)
	renders = append(renders, buf.String())

	secrets := []string{rtsSeedRefresh, "anon-key-SECRET", tok}
	for _, r := range renders {
		for _, secret := range secrets {
			if strings.Contains(r, secret) {
				t.Fatalf("secret %q leaked into %q", secret, r)
			}
		}
	}
}

func TestRefreshingErrorNeverCarriesSecrets(t *testing.T) {
	clock := rtsClock()
	// Error body echoes the seed refresh token (a hostile/verbose endpoint); our
	// typed error must still not surface it.
	stub := &rtsStub{clock: clock, lifetime: time.Hour, status: http.StatusBadRequest, rawBody: `{"error":"invalid_grant","refresh_token":"` + rtsSeedRefresh + `"}`}
	src, _ := rtsNewSource(t, stub)
	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), rtsSeedRefresh) || strings.Contains(err.Error(), "anon-key-SECRET") {
		t.Fatalf("secret leaked into error: %q", err.Error())
	}
}

// TestRefreshingDoesNotFollowRedirectLeakingSecrets is the D1 regression: a 3xx
// from a compromised/MITM/misconfigured token endpoint must NOT be followed, so
// the Supabase apikey and the refresh token never reach the redirect target. The
// exchange fails typed with no cached token instead. It builds the source through
// the constructor (whose client carries CheckRedirect) and, unlike rtsNewSource,
// does NOT override src.http — that is the whole point of the assertion.
func TestRefreshingDoesNotFollowRedirectLeakingSecrets(t *testing.T) {
	var (
		attackerHits atomic.Int64
		amu          sync.Mutex
		gotAPIKey    []string
		gotBodies    []string
	)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHits.Add(1)
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		amu.Lock()
		gotAPIKey = append(gotAPIKey, r.Header.Get("apikey"))
		gotBodies = append(gotBodies, string(b))
		amu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"`+rtsMakeJWT("LEAKED", rtsClock().now().Add(time.Hour).Unix())+`"}`)
	}))
	t.Cleanup(attacker.Close)

	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(token.Close)

	src, err := NewRefreshingTokenSource(token.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(rtsClock().now))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}

	tok, err := src.Token(context.Background())
	if err == nil {
		t.Fatalf("a redirect from the token endpoint must fail the exchange, got token %q", tok)
	}
	if tok != "" {
		t.Fatalf("no token may be returned on a refused redirect, got %q", tok)
	}
	if got := attackerHits.Load(); got != 0 {
		amu.Lock()
		defer amu.Unlock()
		t.Fatalf("redirect was followed: attacker got %d requests; apikeys=%v bodies=%v", got, gotAPIKey, gotBodies)
	}
	var tre *TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *TokenRefreshError", err)
	}
	src.mu.Lock()
	cached := src.accessToken
	src.mu.Unlock()
	if cached != "" {
		t.Fatalf("a refused redirect must leave no cached token, got %q", cached)
	}
}

// TestClientDoesNotFollowRedirectLeakingBearer is the D1 regression for the REST
// client: a 3xx from go-api must not be followed carrying the operator bearer,
// which would turn the client into an SSRF that replays the operator credential to
// the redirect target.
func TestClientDoesNotFollowRedirectLeakingBearer(t *testing.T) {
	var (
		attackerHits atomic.Int64
		amu          sync.Mutex
		gotAuth      []string
	)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHits.Add(1)
		amu.Lock()
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		amu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(attacker.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL, http.StatusFound)
	}))
	t.Cleanup(api.Close)

	client, err := New(api.URL, StaticTokenSource("operator-bearer-SECRET"))
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("a redirect from go-api must not be followed into a success")
	}
	if got := attackerHits.Load(); got != 0 {
		amu.Lock()
		defer amu.Unlock()
		t.Fatalf("client followed the redirect: attacker got %d requests; auth=%v", got, gotAuth)
	}
}

func TestSelectTokenSource(t *testing.T) {
	// Point persistence at a fresh temp dir so the file-store load at construction
	// is hermetic (absent file -> first-boot seed), not the machine's real volume.
	persistPath := filepath.Join(t.TempDir(), "refresh_token")
	refreshEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyRefreshToken:
			return "refresh"
		case envReadOnlyRefreshFile:
			return persistPath
		default:
			return ""
		}
	}
	if _, ok := selectTokenSource(refreshEnv).(*RefreshingTokenSource); !ok {
		t.Fatal("all refresh vars set should select RefreshingTokenSource")
	}

	staticEnv := func(k string) string {
		if k == envReadOnlyToken {
			return "static-readonly-token"
		}
		return ""
	}
	if got, ok := selectTokenSource(staticEnv).(StaticTokenSource); !ok || string(got) != "static-readonly-token" {
		t.Fatalf("only static token set should select StaticTokenSource, got %T", selectTokenSource(staticEnv))
	}

	if _, ok := selectTokenSource(func(string) string { return "" }).(nullTokenSource); !ok {
		t.Fatal("no vars set should select nullTokenSource")
	}

	badURLEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "://bad url"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyRefreshToken:
			return "refresh"
		default:
			return ""
		}
	}
	if _, ok := selectTokenSource(badURLEnv).(nullTokenSource); !ok {
		t.Fatal("refresh vars set but malformed URL should fail closed to nullTokenSource")
	}
}

// TestSelectTokenSource_NoOperatorFallback pins #1810's fail-closed rule: with
// only the pre-#1810 operator credentials in the environment, Overseer takes
// none of them and degrades to source-down. Adopting one would restore exactly
// the write scope the read-only principal exists to drop.
func TestSelectTokenSource_NoOperatorFallback(t *testing.T) {
	operatorEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envLegacyOperatorRefreshToken:
			return "operator-refresh"
		case envLegacyOperatorToken:
			return "operator-bearer"
		default:
			return ""
		}
	}
	if src := selectTokenSource(operatorEnv); !isNullSource(src) {
		t.Fatalf("operator credentials must not be adopted, got %T", src)
	}
}

func isNullSource(src TokenSource) bool {
	_, ok := src.(nullTokenSource)
	return ok
}

// TestRefreshTokenPathIsPrincipalScoped pins that the persisted refresh token
// has the read-only principal's own file. The deployed volume still holds the
// operator chain at the pre-#1810 path, and seedFromStore prefers a persisted
// token over the env seed — so sharing that path would resurrect the operator
// credential one restart after the switch.
func TestRefreshTokenPathIsPrincipalScoped(t *testing.T) {
	const operatorPath = "/var/lib/overseer/refresh_token"

	if got := refreshTokenPath(func(string) string { return "" }); got == operatorPath {
		t.Fatalf("default persistence path = %q, the operator's own file", got)
	}
	override := func(k string) string {
		if k == envReadOnlyRefreshFile {
			return "/tmp/overseer-readonly-token"
		}
		return ""
	}
	if got := refreshTokenPath(override); got != "/tmp/overseer-readonly-token" {
		t.Errorf("path override ignored: got %q", got)
	}
}

func TestNullTokenSourceFailsClosed(t *testing.T) {
	_, err := nullTokenSource{}.Token(context.Background())
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("nullTokenSource error = %v, want ErrNoToken", err)
	}
}

// rtsSwitchStub is a token endpoint whose response is switched between calls: it
// serves failWith (an HTTP status) until a test stores 0, after which it mints a
// valid access token. failWith and the call counter are atomic so -race stays clean
// when a test flips the mode while the source's refresh goroutine is mid-exchange.
type rtsSwitchStub struct {
	clock    *rtsFakeClock
	lifetime time.Duration
	failWith atomic.Int64 // HTTP status served while > 0; 0 mints a valid token
	calls    atomic.Int64
}

func (s *rtsSwitchStub) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := s.calls.Add(1)
	if code := s.failWith.Load(); code != 0 {
		w.WriteHeader(int(code))
		_, _ = io.WriteString(w, `{"error":"refresh_token_already_used"}`)
		return
	}
	exp := s.clock.now().Add(s.lifetime).Unix()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": rtsMakeJWT(fmt.Sprintf("%s-%d", rtsAccessMark, n), exp),
		"token_type":   "bearer",
	})
}

func rtsNewSwitchSource(t *testing.T, stub *rtsSwitchStub) *RefreshingTokenSource {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(stub.clock.now))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()
	src.http.Timeout = 5 * time.Second
	return src
}

// TestRefreshingTerminalFailureBacksOffNotStorms is the ticket's core "Done when":
// a spent seed (Supabase 400 refresh_token_already_used) produces a bounded number
// of exchanges, not one per collect cycle — the storm that Supabase turned into a
// 429 in prod. Buckets keep failing closed on the typed error while the source
// stays quiet.
func TestRefreshingTerminalFailureBacksOffNotStorms(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)

	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("want a refresh error on a 400 from the token endpoint")
	}
	var tre *TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *TokenRefreshError", err)
	}

	// Twenty more collect cycles arrive inside the backoff window: the source must
	// suppress every one of them, not exchange once each.
	for i := 0; i < 20; i++ {
		if _, err := src.Token(context.Background()); err == nil {
			t.Fatal("a backed-off source must keep failing closed, not return a token")
		}
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want 1 (backoff must suppress the retry storm)", got)
	}

	// Once the window elapses, exactly one probe fires — bounded, not a storm.
	clock.advance(refreshBackoffBase + time.Second)
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("still 400: want an error")
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("exchanges = %d, want 2 (one probe per elapsed window)", got)
	}
}

// TestRefreshingRecoversPromptlyAfterReseed is the other half: after a terminal
// 400, a healed endpoint / reseed is exchanged on the next cycle past the window,
// and the success resets backoff so a later failure starts from the short base
// window rather than a wedged-open one.
func TestRefreshingRecoversPromptlyAfterReseed(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want the initial 400 to fail")
	}

	stub.failWith.Store(0) // endpoint heals / operator reseeds
	clock.advance(refreshBackoffBase + time.Second)
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after recovery: %v", err)
	}
	if !strings.Contains(tok, rtsAccessMark) {
		t.Fatalf("token %q does not look like the minted access token", tok)
	}

	src.mu.Lock()
	failCount, retryAt := src.failCount, src.retryAt
	src.mu.Unlock()
	if failCount != 0 || !retryAt.IsZero() {
		t.Fatalf("backoff not reset after recovery: failCount=%d retryAt=%v", failCount, retryAt)
	}
}

// TestRefreshingTransientFailureRecoversNotWedged proves a transient 5xx is not
// mistaken for a terminal failure and permanently wedged: after the endpoint
// recovers, the next attempt past the window succeeds. This is the attack pass's
// "does a 5xx wrongly wedge?" turned into a regression.
func TestRefreshingTransientFailureRecoversNotWedged(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusInternalServerError)
	src := rtsNewSwitchSource(t, stub)

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want the 5xx to fail the first exchange")
	}

	stub.failWith.Store(0) // Supabase recovers
	clock.advance(refreshBackoffBase + time.Second)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("a transient 5xx must recover on retry, not wedge: %v", err)
	}
}

// TestRefreshingBackoffHoldsUnderConcurrency is the attack pass's concurrency
// check: a whole fleet of buckets waking in one collect cycle inside the backoff
// window must not collectively defeat the backoff — single-flight plus the mutex-
// guarded window keep it to zero further exchanges.
func TestRefreshingBackoffHoldsUnderConcurrency(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want the initial 400 to fail")
	}

	const n = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = src.Token(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want 1: backoff + single-flight must survive concurrency", got)
	}
}

// rtsSourceWithStore wires a RefreshingTokenSource at a fresh stub with the given
// persistence store, modelling one process lifetime. Each call returns an
// independent stub so a "restart" (a second call) has its own exchange counter.
func rtsSourceWithStore(t *testing.T, store refreshTokenStore) (*RefreshingTokenSource, *rtsStub) {
	t.Helper()
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: true}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(clock.now), WithRefreshTokenStore(store))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()
	src.http.Timeout = 5 * time.Second
	return src, stub
}

// TestRefreshTokenPersistsAcrossRestart is the ticket's core "Done when": a rotated
// refresh token is written to the durable store on rotation, and a fresh source
// (a restart) reads it and presents it — the spent env seed is never replayed.
func TestRefreshTokenPersistsAcrossRestart(t *testing.T) {
	store := fileRefreshTokenStore{path: filepath.Join(t.TempDir(), "refresh_token")}

	first, firstStub := rtsSourceWithStore(t, store)
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatalf("first process Token: %v", err)
	}
	firstStub.mu.Lock()
	if firstStub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("first exchange used %q, want the seed", firstStub.gotBody[0].RefreshToken)
	}
	firstStub.mu.Unlock()

	persisted, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if string(persisted) != "rotated-refresh-1" {
		t.Fatalf("persisted %q, want the rotated token", string(persisted))
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatalf("stat persisted token: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("persisted token mode = %o, want 600 (never world-readable)", perm)
	}

	second, secondStub := rtsSourceWithStore(t, store)
	if _, err := second.Token(context.Background()); err != nil {
		t.Fatalf("restart Token: %v", err)
	}
	secondStub.mu.Lock()
	defer secondStub.mu.Unlock()
	if secondStub.gotBody[0].RefreshToken != "rotated-refresh-1" {
		t.Fatalf("restart presented %q; want the persisted rotated token, not the spent seed", secondStub.gotBody[0].RefreshToken)
	}
}

// TestRefreshTokenFirstBootSeedsFromEnv is the other half: with no persisted file
// (first boot) the source still seeds from OVERSEER_GOAPI_REFRESH_TOKEN.
func TestRefreshTokenFirstBootSeedsFromEnv(t *testing.T) {
	store := fileRefreshTokenStore{path: filepath.Join(t.TempDir(), "refresh_token")}

	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour} // rotate:false -> nothing to persist
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(clock.now), WithRefreshTokenStore(store))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("first boot used %q, want the env seed", stub.gotBody[0].RefreshToken)
	}
}

// TestFileRefreshTokenStoreLoadErrorContinuesFromSeed: a persistence path that
// cannot be read (here, a directory) must not abort construction. Like a lock or
// mkdir failure, it logs and continues unpersisted from the env seed with
// persistFailed set, so a mis-mounted volume degrades for this process instead of
// bricking the credential until restart.
func TestFileRefreshTokenStoreLoadErrorContinuesFromSeed(t *testing.T) {
	store := fileRefreshTokenStore{path: t.TempDir()} // a directory: ReadFile errors, not ErrNotExist
	src, err := NewRefreshingTokenSource("https://ref.supabase.co", "anon", rtsSeedRefresh, WithRefreshTokenStore(store))
	if err != nil {
		t.Fatalf("an unreadable persistence path must not fail construction, got %v", err)
	}
	if !src.persistFailed {
		t.Fatal("persistFailed must be set when the persisted token cannot be read")
	}
	if src.refreshTok != rtsSeedRefresh {
		t.Fatalf("refreshTok = %q, want the env seed kept in place", src.refreshTok)
	}
}

// TestFileRefreshTokenStoreNeverLeaksToken: the persisted token is written to the
// file only, never to a log line the store or source emits.
func TestFileRefreshTokenStoreNeverLeaksToken(t *testing.T) {
	store := fileRefreshTokenStore{path: filepath.Join(t.TempDir(), "refresh_token")}
	src, _ := rtsSourceWithStore(t, store)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("wired", "source", src)
	for _, secret := range []string{rtsSeedRefresh, "rotated-refresh-1", "anon-key-SECRET"} {
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("secret %q leaked into log %q", secret, buf.String())
		}
	}
}

const burstWaitLimit = 5 * time.Second

func awaitClosed(r *http.Request, ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-r.Context().Done():
		return false
	case <-time.After(burstWaitLimit):
		return false
	}
}

type staleTokenBurstAPI struct {
	stale     string
	burst     int64
	calls     atomic.Int64
	arrivals  atomic.Int64
	allStale  chan struct{}
	freshSeen chan struct{}
	freshOnce sync.Once
}

func newStaleTokenBurstAPI(stale string, burst int64) *staleTokenBurstAPI {
	return &staleTokenBurstAPI{
		stale:     stale,
		burst:     burst,
		allStale:  make(chan struct{}),
		freshSeen: make(chan struct{}),
	}
}

func (a *staleTokenBurstAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.calls.Add(1)
	if presentedToken(r) != a.stale {
		a.freshOnce.Do(func() { close(a.freshSeen) })
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
		return
	}
	arrival := a.arrivals.Add(1)
	if arrival == a.burst {
		close(a.allStale)
	}
	if !awaitClosed(r, a.allStale) {
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	if arrival != 1 && !awaitClosed(r, a.freshSeen) {
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
}

func TestConcurrent401sOnOneTokenShareOneRefreshExchange(t *testing.T) {
	const burst = 10
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	stale, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("prime token: %v", err)
	}
	api := newStaleTokenBurstAPI(stale, burst)
	apiSrv := httptest.NewServer(api)
	t.Cleanup(apiSrv.Close)
	client, err := New(apiSrv.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	errs := make([]error, burst)
	var wg sync.WaitGroup
	for i := range burst {
		wg.Go(func() {
			_, errs[i] = client.AdminHealth(context.Background())
		})
	}
	wg.Wait()

	for i, readErr := range errs {
		if readErr != nil {
			t.Fatalf("request %d: %v", i, readErr)
		}
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("token exchanges = %d, want 2 (prime + one refresh for the whole 401 burst)", got)
	}
	if got := api.calls.Load(); got != 2*burst {
		t.Fatalf("go-api calls = %d, want %d (each request retried at most once)", got, 2*burst)
	}
}

func TestThrottled429TakesNoRetryAndClassifiesThrottled(t *testing.T) {
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	var apiCalls atomic.Int64
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(apiSrv.Close)
	client, err := New(apiSrv.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	_, err = client.AdminHealth(context.Background())

	if reason := Classify(err); reason != ReasonThrottled {
		t.Fatalf("Classify(%v) = %q, want %q", err, reason, ReasonThrottled)
	}
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("go-api calls = %d, want 1 (a 429 is never retried)", got)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("token exchanges = %d, want 1 (a 429 never invalidates the token)", got)
	}
}

func TestLate401OnSupersededTokenKeepsTheFreshToken(t *testing.T) {
	stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)
	stale, _ := src.Token(context.Background())
	invalidateOn401(src, http.StatusUnauthorized, stale)
	fresh, _ := src.Token(context.Background())

	invalidateOn401(src, http.StatusUnauthorized, stale)
	after, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after late 401: %v", err)
	}

	if after != fresh {
		t.Fatal("a late 401 on the superseded token discarded the fresh one")
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("token exchanges = %d, want 2 (prime + one refresh)", got)
	}
}

type sseConnector interface {
	connect(ctx context.Context) (*http.Response, error)
}

func TestStreamConnect401DiscardsThePresentedToken(t *testing.T) {
	always401 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	consumers := map[string]func(base string, tokens TokenSource) (sseConnector, error){
		"events": func(base string, tokens TokenSource) (sseConnector, error) { return NewConsumer(base, tokens) },
		"logs":   func(base string, tokens TokenSource) (sseConnector, error) { return NewLogsConsumer(base, tokens) },
	}
	for name, build := range consumers {
		t.Run(name, func(t *testing.T) {
			stub := &rtsStub{clock: rtsClock(), lifetime: time.Hour}
			src, _ := rtsNewSource(t, stub)
			rejected, _ := src.Token(context.Background())
			apiSrv := httptest.NewServer(always401)
			t.Cleanup(apiSrv.Close)
			consumer, err := build(apiSrv.URL, src)
			if err != nil {
				t.Fatalf("build consumer: %v", err)
			}

			resp, err := consumer.connect(context.Background())
			if resp != nil {
				_ = resp.Body.Close()
			}

			if Classify(err) != ReasonAuth {
				t.Fatalf("connect err = %v, want an auth rejection", err)
			}
			next, _ := src.Token(context.Background())
			if next == rejected || next == "" {
				t.Fatal("the reconnect would re-present the token go-api just rejected")
			}
		})
	}
}

// TestRefreshingServesCachedTokenPastRefreshAtDuringBackoff is the ticket's core
// "Done when": once the proactive-refresh window passes but the token has not
// actually expired, a failing refresh must not throw the still-valid token away —
// Token keeps serving it through the backoff, rather than every caller failing
// closed for up to the backoff window.
func TestRefreshingServesCachedTokenPastRefreshAtDuringBackoff(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	src := rtsNewSwitchSource(t, stub)

	first, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("initial Token: %v", err)
	}

	stub.failWith.Store(http.StatusInternalServerError)
	clock.advance(49 * time.Minute) // past the 48m (80%) refreshAt, before the 60m exp

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token past refreshAt but before exp, refresh failing: %v", err)
	}
	if tok != first {
		t.Fatalf("expected the still-valid cached token to be served, got a different one")
	}

	clock.advance(12 * time.Minute) // now past the 60m exp
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want an error once the cached token has actually expired")
	}
}

type rtsRotatingStub struct {
	clock     *rtsFakeClock
	mu        sync.Mutex
	presented []string
	respond   func(presented string, n int) (status int, rotated string)
}

func (s *rtsRotatingStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body refreshGrantBody
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	s.mu.Lock()
	s.presented = append(s.presented, body.RefreshToken)
	n := len(s.presented)
	s.mu.Unlock()

	status, rotated := s.respond(body.RefreshToken, n)
	if status != http.StatusOK {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"refresh_token_already_used"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  rtsMakeJWT(fmt.Sprintf("%s-%d", rtsAccessMark, n), s.clock.now().Add(time.Hour).Unix()),
		"refresh_token": rotated,
	})
}

func (s *rtsRotatingStub) presentedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.presented...)
}

func rtsSourceOnFile(t *testing.T, srv *httptest.Server, clock *rtsFakeClock, store refreshTokenStore) *RefreshingTokenSource {
	t.Helper()
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, withClock(clock.now), WithRefreshTokenStore(store))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()
	src.http.Timeout = 5 * time.Second
	return src
}

func rtsTokenFile(t *testing.T) fileRefreshTokenStore {
	t.Helper()
	return fileRefreshTokenStore{path: filepath.Join(t.TempDir(), "readonly_refresh_token")}
}

func TestSpentRefreshTokenAdoptsTheNewerPersistedTokenWithoutBackoff(t *testing.T) {
	clock := rtsClock()
	store := rtsTokenFile(t)
	const newer = "newer-refresh-from-another-writer"
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(presented string, n int) (int, string) {
		if presented != newer {
			if err := os.WriteFile(store.path, []byte(newer), 0o600); err != nil {
				t.Errorf("write newer token: %v", err)
			}
			return http.StatusBadRequest, ""
		}
		return http.StatusOK, fmt.Sprintf("rotated-refresh-%d", n)
	}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src := rtsSourceOnFile(t, srv, clock, store)

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v, want the newer persisted token exchanged", err)
	}
	if !strings.Contains(tok, rtsAccessMark) {
		t.Fatalf("token %q is not a minted access token", tok)
	}
	if got := stub.presentedTokens(); len(got) != 2 || got[0] != rtsSeedRefresh || got[1] != newer {
		t.Fatalf("presented %q, want the spent memory token then the newer persisted one", got)
	}
	src.mu.Lock()
	failCount, backingOff := src.failCount, src.inBackoffLocked()
	src.mu.Unlock()
	if failCount != 0 || backingOff {
		t.Fatalf("failCount=%d backingOff=%v, want no backoff after adopting the newer token", failCount, backingOff)
	}
}

func TestSpentRefreshTokenWithNothingNewerPersistedBacksOffAfterOneExchange(t *testing.T) {
	clock := rtsClock()
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(string, int) (int, string) { return http.StatusBadRequest, "" }
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src := rtsSourceOnFile(t, srv, clock, rtsTokenFile(t))

	_, err := src.Token(context.Background())

	if !isSpentRefreshToken(err) {
		t.Fatalf("err = %v, want the 400 surfaced", err)
	}
	if got := stub.presentedTokens(); len(got) != 1 {
		t.Fatalf("presented %q, want exactly one exchange when the file holds nothing newer", got)
	}
	src.mu.Lock()
	backingOff := src.inBackoffLocked()
	src.mu.Unlock()
	if !backingOff {
		t.Fatal("want backoff after a 400 with no newer persisted token")
	}
}

func TestTwoSourcesOnOneFileNeverExchangeTheSameRefreshToken(t *testing.T) {
	clock := rtsClock()
	store := rtsTokenFile(t)
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(_ string, n int) (int, string) {
		time.Sleep(5 * time.Millisecond)
		return http.StatusOK, fmt.Sprintf("rotated-refresh-%d", n)
	}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	sources := []*RefreshingTokenSource{
		rtsSourceOnFile(t, srv, clock, store),
		rtsSourceOnFile(t, srv, clock, store),
	}
	const rounds = 10

	for round := 0; round < rounds; round++ {
		clock.advance(2 * time.Hour)
		var wg sync.WaitGroup
		for _, src := range sources {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := src.Token(context.Background()); err != nil {
					t.Errorf("round %d Token: %v", round, err)
				}
			}()
		}
		wg.Wait()
	}

	presented := stub.presentedTokens()
	seen := map[string]bool{}
	for _, refreshTok := range presented {
		if seen[refreshTok] {
			t.Fatalf("refresh token %q exchanged twice; presented %q", refreshTok, presented)
		}
		seen[refreshTok] = true
	}
	if got := len(presented); got != len(sources)*rounds {
		t.Fatalf("exchanges = %d, want %d (every refresh of both sources)", got, len(sources)*rounds)
	}
}

type rtsSaveFailingStore struct {
	fileRefreshTokenStore
}

func (rtsSaveFailingStore) save(string) error { return errors.New("disk full") }

func TestFailedSaveSetsPersistFailed(t *testing.T) {
	src, _ := rtsSourceWithStore(t, rtsSaveFailingStore{rtsTokenFile(t)})

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v, a failed save must not fail the exchange", err)
	}

	src.mu.Lock()
	persistFailed := src.persistFailed
	src.mu.Unlock()
	if !persistFailed {
		t.Fatal("persistFailed = false after the rotated token could not be saved")
	}
}

func TestFailedSaveKeepsPresentingTheInMemoryRotation(t *testing.T) {
	store := rtsTokenFile(t)
	if err := os.WriteFile(store.path, []byte("persisted-before-the-disk-filled"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}
	src, stub := rtsSourceWithStore(t, rtsSaveFailingStore{store})
	first, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}

	src.invalidateRejected(first)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("second Token: %v", err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if got := stub.gotBody[1].RefreshToken; got != "rotated-refresh-1" {
		t.Fatalf("second exchange presented %q, want the unsaved in-memory rotation", got)
	}
}

func TestSuccessfulSaveClearsPersistFailed(t *testing.T) {
	src, _ := rtsSourceWithStore(t, rtsTokenFile(t))
	src.persistFailed = true

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	src.mu.Lock()
	persistFailed := src.persistFailed
	src.mu.Unlock()
	if persistFailed {
		t.Fatal("persistFailed still set after a successful save")
	}
}

func TestLockFileSitsBesideTheTokenAtOwnerOnlyMode(t *testing.T) {
	store := rtsTokenFile(t)
	src, _ := rtsSourceWithStore(t, store)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	info, err := os.Stat(store.path + ".lock")
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock file mode = %o, want 600", perm)
	}
}

func TestUncreatableTokenDirAtStartupStillServesFromTheEnvSeed(t *testing.T) {
	clock := rtsClock()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	store := fileRefreshTokenStore{path: filepath.Join(blocker, "overseer", "readonly_refresh_token")}
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(_ string, n int) (int, string) { return http.StatusOK, fmt.Sprintf("rotated-refresh-%d", n) }
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src := rtsSourceOnFile(t, srv, clock, store)

	token, err := src.Token(context.Background())

	if err != nil || token == "" {
		t.Fatalf("Token = %q, %v; want an access token from the env seed", token, err)
	}
	if got := stub.presentedTokens(); len(got) != 1 || got[0] != rtsSeedRefresh {
		t.Fatalf("presented %q, want exactly the env seed", got)
	}
	src.mu.Lock()
	persistFailed := src.persistFailed
	src.mu.Unlock()
	if !persistFailed {
		t.Fatal("persistFailed = false although the token directory cannot be created")
	}
}

const (
	pgEmail    = "overseer-readonly@altune.test"
	pgPassword = "readonly-PASSWORD-SECRET-42"
	pgSignedIn = "refresh-from-password-grant-SECRET"
)

type pgStub struct {
	clock          *rtsFakeClock
	refreshStatus  atomic.Int64
	passwordStatus atomic.Int64
	refreshCalls   atomic.Int64
	passwordCalls  atomic.Int64
	mu             sync.Mutex
	presented      []string
	signIns        []passwordGrantBody
	apiKeys        []string
}

func newPGStub(clock *rtsFakeClock) *pgStub {
	stub := &pgStub{clock: clock}
	stub.refreshStatus.Store(http.StatusBadRequest)
	stub.passwordStatus.Store(http.StatusOK)
	return stub
}

func (s *pgStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	s.mu.Lock()
	s.apiKeys = append(s.apiKeys, r.Header.Get("apikey"))
	s.mu.Unlock()
	switch r.URL.Query().Get("grant_type") {
	case "password":
		s.servePassword(w, raw)
	case "refresh_token":
		s.serveRefresh(w, raw)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *pgStub) serveRefresh(w http.ResponseWriter, raw []byte) {
	n := s.refreshCalls.Add(1)
	var body refreshGrantBody
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.presented = append(s.presented, body.RefreshToken)
	s.mu.Unlock()
	if body.RefreshToken != pgSignedIn && !strings.HasPrefix(body.RefreshToken, "rotated-after-") {
		w.WriteHeader(int(s.refreshStatus.Load()))
		_, _ = io.WriteString(w, `{"error":"refresh_token_already_used"}`)
		return
	}
	s.writeTokens(w, fmt.Sprintf("refresh-%d", n), fmt.Sprintf("rotated-after-%d", n))
}

func (s *pgStub) servePassword(w http.ResponseWriter, raw []byte) {
	n := s.passwordCalls.Add(1)
	var body passwordGrantBody
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.signIns = append(s.signIns, body)
	s.mu.Unlock()
	if code := s.passwordStatus.Load(); code != http.StatusOK {
		w.WriteHeader(int(code))
		_, _ = io.WriteString(w, `{"error":"invalid_grant","password":"`+pgPassword+`"}`)
		return
	}
	s.writeTokens(w, fmt.Sprintf("password-%d", n), pgSignedIn)
}

func (s *pgStub) writeTokens(w http.ResponseWriter, mark, refresh string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  rtsMakeJWT(rtsAccessMark+"-"+mark, s.clock.now().Add(time.Hour).Unix()),
		"refresh_token": refresh,
	})
}

func (s *pgStub) presentedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.presented...)
}

func (s *pgStub) signInBodies() []passwordGrantBody {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]passwordGrantBody(nil), s.signIns...)
}

func pgSource(t *testing.T, stub *pgStub, seed string, opts ...RefreshingOption) *RefreshingTokenSource {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	opts = append(opts, withClock(stub.clock.now))
	src, err := NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", seed, opts...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	src.http = srv.Client()
	src.http.Timeout = 5 * time.Second
	return src
}

func pgCaptureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestSpentRefreshTokenSignsInAgainAndPersistsTheNewChain(t *testing.T) {
	clock := rtsClock()
	stub := newPGStub(clock)
	store := rtsTokenFile(t)
	src := pgSource(t, stub, rtsSeedRefresh, WithRefreshTokenStore(store), WithPasswordGrant(pgEmail, pgPassword))

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after a spent refresh token with credentials set: %v", err)
	}
	if !strings.Contains(tok, "password-1") {
		t.Fatalf("token %q was not minted by the password grant", tok)
	}
	signIns := stub.signInBodies()
	if len(signIns) != 1 || signIns[0].Email != pgEmail || signIns[0].Password != pgPassword {
		t.Fatalf("password grants = %+v, want exactly one carrying the read-only credentials", signIns)
	}
	persisted, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if string(persisted) != pgSignedIn {
		t.Fatalf("persisted %q, want the refresh token the password grant returned", persisted)
	}

	clock.advance(50 * time.Minute)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("next Token after the sign-in: %v", err)
	}
	presented := stub.presentedTokens()
	if last := presented[len(presented)-1]; last != pgSignedIn {
		t.Fatalf("next refresh presented %q, want the signed-in refresh token", last)
	}
	if got := stub.passwordCalls.Load(); got != 1 {
		t.Fatalf("password grants = %d, want 1: a live chain must not sign in again", got)
	}
	for _, key := range stub.apiKeys {
		if key != "anon-key-SECRET" {
			t.Fatalf("apikey header = %q, want the anon key on every grant", key)
		}
	}
}

func TestSpentRefreshTokenWithoutCredentialsBacksOffWithoutSigningIn(t *testing.T) {
	clock := rtsClock()
	stub := newPGStub(clock)
	src := pgSource(t, stub, rtsSeedRefresh, WithPasswordGrant("", ""))

	var refreshErr *TokenRefreshError
	if _, err := src.Token(context.Background()); !errors.As(err, &refreshErr) || refreshErr.Status != http.StatusBadRequest {
		t.Fatalf("Token = %v, want the refresh 400", err)
	}
	for i := 0; i < 10; i++ {
		_, _ = src.Token(context.Background())
	}
	if got := stub.refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh exchanges = %d, want 1 inside the backoff window", got)
	}
	if got := stub.passwordCalls.Load(); got != 0 {
		t.Fatalf("password grants = %d, want 0 without credentials", got)
	}
}

func TestFailingPasswordGrantBacksOffAndNeverLogsThePassword(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			logs := pgCaptureLogs(t)
			clock := rtsClock()
			stub := newPGStub(clock)
			stub.passwordStatus.Store(int64(status))
			src := pgSource(t, stub, rtsSeedRefresh, WithRefreshTokenStore(rtsTokenFile(t)), WithPasswordGrant(pgEmail, pgPassword))

			_, err := src.Token(context.Background())
			var refreshErr *TokenRefreshError
			if !errors.As(err, &refreshErr) || refreshErr.Stage != "password_grant" || refreshErr.Status != status {
				t.Fatalf("Token = %v, want a password_grant failure with status %d", err, status)
			}
			for i := 0; i < 20; i++ {
				if _, err := src.Token(context.Background()); err == nil {
					t.Fatal("a backed-off source must keep failing closed")
				}
			}
			if got := stub.passwordCalls.Load(); got != 1 {
				t.Fatalf("password grants = %d, want 1 per backoff window", got)
			}

			clock.advance(refreshBackoffMax)
			_, _ = src.Token(context.Background())
			if got := stub.passwordCalls.Load(); got != 2 {
				t.Fatalf("password grants = %d, want one more once the window elapsed", got)
			}

			logs.WriteString(src.String())
			slog.Info("wired", "source", src)
			for _, secret := range []string{pgPassword, rtsSeedRefresh, "anon-key-SECRET"} {
				if strings.Contains(logs.String(), secret) || strings.Contains(err.Error(), secret) {
					t.Fatalf("secret %q leaked into logs or error: %s", secret, logs.String())
				}
			}
			if !strings.Contains(logs.String(), "read-only token refresh failed at password_grant") {
				t.Fatalf("failed sign-in not logged under the deploy gate's signature: %s", logs.String())
			}
		})
	}
}

func TestSignInRunsOnceForAConcurrentFleet(t *testing.T) {
	clock := rtsClock()
	stub := newPGStub(clock)
	src := pgSource(t, stub, rtsSeedRefresh, WithRefreshTokenStore(rtsTokenFile(t)), WithPasswordGrant(pgEmail, pgPassword))

	const fleet = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < fleet; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = src.Token(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	if got := stub.passwordCalls.Load(); got != 1 {
		t.Fatalf("password grants = %d, want 1 for the whole fleet", got)
	}
}

func TestPasswordGrantAloneStartsTheChainWithoutASeed(t *testing.T) {
	clock := rtsClock()
	stub := newPGStub(clock)
	store := rtsTokenFile(t)
	src := pgSource(t, stub, "", WithRefreshTokenStore(store), WithPasswordGrant(pgEmail, pgPassword))

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token with credentials and no seed: %v", err)
	}
	if got := stub.refreshCalls.Load(); got != 0 {
		t.Fatalf("refresh exchanges = %d, want 0 when no refresh token is held", got)
	}
	persisted, _ := os.ReadFile(store.path)
	if string(persisted) != pgSignedIn {
		t.Fatalf("persisted %q, want the signed-in refresh token", persisted)
	}
}

func TestNoSeedAndNoCredentialsStillFailsConstruction(t *testing.T) {
	if _, err := NewRefreshingTokenSource("https://ref.supabase.co", "anon", "", WithPasswordGrant(pgEmail, "")); err == nil {
		t.Fatal("a source with neither a refresh token nor a full credential pair must fail construction")
	}
}

func TestSelectTokenSourceRefreshesWithCredentialsAndNoSeed(t *testing.T) {
	persistPath := rtsTokenFile(t).path
	credentialsEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyEmail:
			return pgEmail
		case envReadOnlyPassword:
			return pgPassword
		case envReadOnlyRefreshFile:
			return persistPath
		default:
			return ""
		}
	}
	src, ok := selectTokenSource(credentialsEnv).(*RefreshingTokenSource)
	if !ok {
		t.Fatalf("credentials without a seed should select RefreshingTokenSource, got %T", selectTokenSource(credentialsEnv))
	}
	if src.signIn == nil {
		t.Fatal("selectTokenSource did not pass the password grant to the source")
	}

	halfEnv := func(k string) string {
		if k == envReadOnlyPassword {
			return ""
		}
		return credentialsEnv(k)
	}
	if !isNullSource(selectTokenSource(halfEnv)) {
		t.Fatal("an email without a password and no seed must stay source-down")
	}
}

func TestUnreadableTokenFileAtStartupStillServesFromTheEnvSeed(t *testing.T) {
	clock := rtsClock()
	store := rtsTokenFile(t)
	if err := os.WriteFile(store.path, []byte("cannot-read-me"), 0o000); err != nil {
		t.Fatalf("seed unreadable token file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.path, 0o600) })
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(_ string, n int) (int, string) { return http.StatusOK, fmt.Sprintf("rotated-refresh-%d", n) }
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src := rtsSourceOnFile(t, srv, clock, store)

	src.mu.Lock()
	persistFailedAtConstruction := src.persistFailed
	src.mu.Unlock()
	if !persistFailedAtConstruction {
		t.Fatal("persistFailed = false right after construction although the persisted token file could not be read")
	}

	token, err := src.Token(context.Background())

	if err != nil || token == "" {
		t.Fatalf("Token = %q, %v; want an access token from the env seed", token, err)
	}
	if got := stub.presentedTokens(); len(got) != 1 || got[0] != rtsSeedRefresh {
		t.Fatalf("presented %q, want exactly the env seed", got)
	}
}
