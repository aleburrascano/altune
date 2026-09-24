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
	src.invalidate() // models the client dropping a 401'd token before its window
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
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(api.Close)
	_ = tokenSrv

	client, err := New(api.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health after 401 retry: %v", err)
	}
	if !health.OK() {
		t.Fatalf("health = %+v, want ok", health)
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
