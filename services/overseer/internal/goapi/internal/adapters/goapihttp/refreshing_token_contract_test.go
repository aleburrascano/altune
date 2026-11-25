package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
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

const (
	rtsSeedRefresh = "seed-refresh-token-SECRET-000"
	rtsAccessMark  = "ACCESS-TOKEN-SECRET"
)

const (
	rtsBackoffBase = time.Second
	rtsBackoffMax  = 5 * time.Minute
)

type rtsRefreshGrant struct {
	RefreshToken string `json:"refresh_token"`
}

type pgSignInGrant struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func bearerOf(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func rtsHTTPClient(srv *httptest.Server) *http.Client {
	client := srv.Client()
	client.Timeout = 5 * time.Second
	return client
}

func rtsOptions(srv *httptest.Server, clock *rtsFakeClock, opts ...goapihttp.RefreshingOption) []goapihttp.RefreshingOption {
	return append([]goapihttp.RefreshingOption{goapihttp.WithClock(clock.now), goapihttp.WithRefreshHTTPClient(rtsHTTPClient(srv))}, opts...)
}

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

func rtsMakeJWT(mark string, expUnix int64) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"mark":%q}`, expUnix, mark)))
	return hdr + "." + payload + ".sig-" + mark
}

type rtsStub struct {
	clock     *rtsFakeClock
	lifetime  time.Duration
	calls     atomic.Int64
	release   chan struct{}
	mu        sync.Mutex
	gotAPIKey []string
	gotBody   []rtsRefreshGrant
	status    int
	rawBody   string
	rotate    bool
}

func (s *rtsStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.release != nil {
		<-s.release
	}
	n := s.calls.Add(1)

	var body rtsRefreshGrant
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
	rtsWriteJSON(w, resp)
}

func rtsWriteJSON(w http.ResponseWriter, v map[string]any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func rtsNewSource(t *testing.T, stub *rtsStub) (*goapihttp.RefreshingTokenSource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, rtsOptions(srv, stub.clock)...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
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
	clock.advance(30 * time.Minute)
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
	clock.advance(47 * time.Minute)
	again, _ := src.Token(context.Background())
	if again != first || stub.calls.Load() != 1 {
		t.Fatalf("token refreshed too early: calls=%d", stub.calls.Load())
	}
	clock.advance(2 * time.Minute)
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

func rejectToken(t *testing.T, src goapi.TokenSource, rejected string) {
	t.Helper()
	var rejections atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearerOf(r) == rejected {
			rejections.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
	}))
	defer api.Close()
	client, err := goapihttp.New(api.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	if _, err := client.AdminHealth(context.Background()); err != nil {
		t.Fatalf("AdminHealth after go-api rejected the token: %v", err)
	}
	if rejections.Load() != 1 {
		t.Fatalf("go-api rejected %d requests, want the one that presented the token", rejections.Load())
	}
}

func TestRefreshingRefreshOnInvalidate(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	src, _ := rtsNewSource(t, stub)

	first, _ := src.Token(context.Background())
	rejectToken(t, src, first)
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
		_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
	}))
	t.Cleanup(api.Close)
	_ = tokenSrv

	client, err := goapihttp.New(api.URL, src)
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
	var tre *goapihttp.TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *goapihttp.TokenRefreshError", err)
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
				var tre *goapihttp.TokenRefreshError
				if !errors.As(err, &tre) {
					t.Fatalf("error %v is not a *goapihttp.TokenRefreshError", err)
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
	clock := rtsClock()
	huge := `{"access_token":"` + rtsMakeJWT("huge", 1<<62) + `"}`
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rawBody: huge}
	src, _ := rtsNewSource(t, stub)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("huge exp should be accepted with a capped window, got %v", err)
	}
	clock.advance(19 * time.Hour)
	if _, err := src.Token(context.Background()); err != nil || stub.calls.Load() != 1 {
		t.Fatalf("19h in: err %v exchanges %d, want the cached token inside 4/5 of the 24h cap", err, stub.calls.Load())
	}
	clock.advance(time.Hour)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token past the capped window: %v", err)
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("exchanges = %d 20h in, want 2: the refresh window must be capped at the 24h lifetime", got)
	}
}

func TestRefreshingRotationPersistsAndNeverBricks(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: true}
	src, _ := rtsNewSource(t, stub)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("first Token: %v", err)
	}
	clock.advance(50 * time.Minute)
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
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: false}
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

	renders := []string{
		src.String(),
		fmt.Sprintf("%v", src),
		fmt.Sprintf("%+v", src),
		fmt.Sprintf("wired source: %s", src),
	}
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

	src, err := goapihttp.NewRefreshingTokenSource(token.URL, "anon-key-SECRET", rtsSeedRefresh, goapihttp.WithClock(rtsClock().now))
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
	var tre *goapihttp.TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *goapihttp.TokenRefreshError", err)
	}
	if cached, err := src.Token(context.Background()); err == nil || cached != "" {
		t.Fatalf("a refused redirect must leave no cached token, got %q (err %v)", cached, err)
	}
}

type rtsSwitchStub struct {
	clock    *rtsFakeClock
	lifetime time.Duration
	failWith atomic.Int64
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
	rtsWriteJSON(w, map[string]any{
		"access_token": rtsMakeJWT(fmt.Sprintf("%s-%d", rtsAccessMark, n), exp),
		"token_type":   "bearer",
	})
}

func rtsNewSwitchSource(t *testing.T, stub *rtsSwitchStub, opts ...goapihttp.RefreshingOption) *goapihttp.RefreshingTokenSource {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, rtsOptions(srv, stub.clock, opts...)...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src
}

func TestRefreshingTerminalFailureBacksOffNotStorms(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)

	_, err := src.Token(context.Background())
	if err == nil {
		t.Fatal("want a refresh error on a 400 from the token endpoint")
	}
	var tre *goapihttp.TokenRefreshError
	if !errors.As(err, &tre) {
		t.Fatalf("error %v is not a *goapihttp.TokenRefreshError", err)
	}

	for i := 0; i < 20; i++ {
		if _, err := src.Token(context.Background()); err == nil {
			t.Fatal("a backed-off source must keep failing closed, not return a token")
		}
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("exchanges = %d, want 1 (backoff must suppress the retry storm)", got)
	}

	clock.advance(rtsBackoffBase + time.Second)
	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("still 400: want an error")
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("exchanges = %d, want 2 (one probe per elapsed window)", got)
	}
}

func TestRefreshingRecoversPromptlyAfterReseed(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want the initial 400 to fail")
	}

	stub.failWith.Store(0)
	clock.advance(rtsBackoffBase + time.Second)
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after recovery: %v", err)
	}
	if !strings.Contains(tok, rtsAccessMark) {
		t.Fatalf("token %q does not look like the minted access token", tok)
	}

	if health := src.Health(); health.ConsecutiveFailures != 0 || health.LastError != "" {
		t.Fatalf("backoff not reset after recovery: %+v", health)
	}
}

func TestRefreshingTransientFailureRecoversNotWedged(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusInternalServerError)
	src := rtsNewSwitchSource(t, stub)

	if _, err := src.Token(context.Background()); err == nil {
		t.Fatal("want the 5xx to fail the first exchange")
	}

	stub.failWith.Store(0)
	clock.advance(rtsBackoffBase + time.Second)
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("a transient 5xx must recover on retry, not wedge: %v", err)
	}
}

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

func rtsSourceWithStore(t *testing.T, store goapihttp.RefreshTokenStore) (*goapihttp.RefreshingTokenSource, *rtsStub) {
	t.Helper()
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour, rotate: true}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, rtsOptions(srv, clock, goapihttp.WithRefreshTokenStore(store))...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src, stub
}

func TestRefreshTokenPersistsAcrossRestart(t *testing.T) {
	store := goapihttp.FileRefreshTokenStore{Path: filepath.Join(t.TempDir(), "refresh_token")}

	first, firstStub := rtsSourceWithStore(t, store)
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatalf("first process Token: %v", err)
	}
	firstStub.mu.Lock()
	if firstStub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("first exchange used %q, want the seed", firstStub.gotBody[0].RefreshToken)
	}
	firstStub.mu.Unlock()

	persisted, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	if string(persisted) != "rotated-refresh-1" {
		t.Fatalf("persisted %q, want the rotated token", string(persisted))
	}
	info, err := os.Stat(store.Path)
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

func TestRefreshTokenFirstBootSeedsFromEnv(t *testing.T) {
	store := goapihttp.FileRefreshTokenStore{Path: filepath.Join(t.TempDir(), "refresh_token")}

	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh,
		goapihttp.WithClock(clock.now), goapihttp.WithRefreshHTTPClient(srv.Client()), goapihttp.WithRefreshTokenStore(store))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("first boot used %q, want the env seed", stub.gotBody[0].RefreshToken)
	}
}

func TestFileRefreshTokenStoreLoadErrorContinuesFromSeed(t *testing.T) {
	clock := rtsClock()
	stub := &rtsStub{clock: clock, lifetime: time.Hour}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	store := goapihttp.FileRefreshTokenStore{Path: t.TempDir()}
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon", rtsSeedRefresh, rtsOptions(srv, clock, goapihttp.WithRefreshTokenStore(store))...)
	if err != nil {
		t.Fatalf("an unreadable persistence path must not fail construction, got %v", err)
	}
	if !src.Health().PersistFailed {
		t.Fatal("PersistFailed must be set when the persisted token cannot be read")
	}
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.gotBody[0].RefreshToken != rtsSeedRefresh {
		t.Fatalf("presented %q, want the env seed kept in place", stub.gotBody[0].RefreshToken)
	}
}

func TestFileRefreshTokenStoreNeverLeaksToken(t *testing.T) {
	store := goapihttp.FileRefreshTokenStore{Path: filepath.Join(t.TempDir(), "refresh_token")}
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
	if bearerOf(r) != a.stale {
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
	client, err := goapihttp.New(apiSrv.URL, src)
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
	client, err := goapihttp.New(apiSrv.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}

	_, err = client.AdminHealth(context.Background())

	if reason := goapi.Classify(err); reason != goapi.ReasonThrottled {
		t.Fatalf("goapi.Classify(%v) = %q, want %q", err, reason, goapi.ReasonThrottled)
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
	lateArrived := make(chan struct{})
	releaseLate := make(chan struct{})
	var staleCalls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearerOf(r) != stale {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"db":"ok","redis":"ok","auth":"ok"}`)
			return
		}
		if staleCalls.Add(1) == 1 {
			close(lateArrived)
			if !awaitClosed(r, releaseLate) {
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(api.Close)
	client, err := goapihttp.New(api.URL, src)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	lateErr := make(chan error, 1)
	go func() {
		_, err := client.AdminHealth(context.Background())
		lateErr <- err
	}()
	select {
	case <-lateArrived:
	case <-time.After(burstWaitLimit):
		t.Fatal("the late request never reached go-api")
	}

	if _, err := client.AdminHealth(context.Background()); err != nil {
		t.Fatalf("request rejected on the stale token: %v", err)
	}
	fresh, _ := src.Token(context.Background())
	close(releaseLate)
	if err := <-lateErr; err != nil {
		t.Fatalf("late request after its 401: %v", err)
	}
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

func TestStreamConnect401DiscardsThePresentedToken(t *testing.T) {
	always401 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	consumers := map[string]func(base string, tokens goapi.TokenSource) (streamConsumer, error){
		"events": func(base string, tokens goapi.TokenSource) (streamConsumer, error) {
			return goapihttp.NewConsumer(base, tokens, goapihttp.WithBackoff(fixedBackoff(time.Hour)))
		},
		"logs": func(base string, tokens goapi.TokenSource) (streamConsumer, error) {
			return goapihttp.NewLogsConsumer(base, tokens, goapihttp.WithLogsBackoff(fixedBackoff(time.Hour)))
		},
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

			stop := startConsumer(consumer)
			waitUntil(t, func() bool { return consumer.LastError() != nil }, "the first connect to fail")
			stop()

			if err := consumer.LastError(); goapi.Classify(err) != goapi.ReasonAuth {
				t.Fatalf("connect err = %v, want an auth rejection", err)
			}
			next, _ := src.Token(context.Background())
			if next == rejected || next == "" {
				t.Fatal("the reconnect would re-present the token go-api just rejected")
			}
		})
	}
}

func TestRefreshingServesCachedTokenPastRefreshAtDuringBackoff(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	src := rtsNewSwitchSource(t, stub)

	first, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("initial Token: %v", err)
	}

	stub.failWith.Store(http.StatusInternalServerError)
	clock.advance(49 * time.Minute)

	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token past refreshAt but before exp, refresh failing: %v", err)
	}
	if tok != first {
		t.Fatalf("expected the still-valid cached token to be served, got a different one")
	}

	clock.advance(12 * time.Minute)
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
	var body rtsRefreshGrant
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
	rtsWriteJSON(w, map[string]any{
		"access_token":  rtsMakeJWT(fmt.Sprintf("%s-%d", rtsAccessMark, n), s.clock.now().Add(time.Hour).Unix()),
		"refresh_token": rotated,
	})
}

func (s *rtsRotatingStub) presentedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.presented...)
}

func rtsSourceOnFile(t *testing.T, srv *httptest.Server, clock *rtsFakeClock, store goapihttp.RefreshTokenStore) *goapihttp.RefreshingTokenSource {
	t.Helper()
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", rtsSeedRefresh, rtsOptions(srv, clock, goapihttp.WithRefreshTokenStore(store))...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src
}

func rtsTokenFile(t *testing.T) goapihttp.FileRefreshTokenStore {
	t.Helper()
	return goapihttp.FileRefreshTokenStore{Path: filepath.Join(t.TempDir(), "readonly_refresh_token")}
}

func TestSpentRefreshTokenAdoptsTheNewerPersistedTokenWithoutBackoff(t *testing.T) {
	clock := rtsClock()
	store := rtsTokenFile(t)
	const newer = "newer-refresh-from-another-writer"
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(presented string, n int) (int, string) {
		if presented != newer {
			if err := os.WriteFile(store.Path, []byte(newer), 0o600); err != nil {
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
	if health := src.Health(); health.ConsecutiveFailures != 0 || health.LastError != "" {
		t.Fatalf("health = %+v, want no backoff after adopting the newer token", health)
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

	var refreshErr *goapihttp.TokenRefreshError
	if !errors.As(err, &refreshErr) || refreshErr.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want the 400 surfaced", err)
	}
	if got := stub.presentedTokens(); len(got) != 1 {
		t.Fatalf("presented %q, want exactly one exchange when the file holds nothing newer", got)
	}
	if _, err := src.Token(context.Background()); err == nil || len(stub.presentedTokens()) != 1 {
		t.Fatalf("Token = %v after %d exchanges, want backoff after a 400 with no newer persisted token", err, len(stub.presentedTokens()))
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
	sources := []*goapihttp.RefreshingTokenSource{
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
	goapihttp.FileRefreshTokenStore
}

func (rtsSaveFailingStore) Save(string) error { return errors.New("disk full") }

type rtsFirstLoadFailingStore struct {
	goapihttp.FileRefreshTokenStore
	loads *atomic.Int64
}

func (s rtsFirstLoadFailingStore) Load() (string, error) {
	if s.loads.Add(1) == 1 {
		return "", errors.New("transient read error")
	}
	return s.FileRefreshTokenStore.Load()
}

func TestFailedSaveSetsPersistFailed(t *testing.T) {
	src, _ := rtsSourceWithStore(t, rtsSaveFailingStore{rtsTokenFile(t)})

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v, a failed save must not fail the exchange", err)
	}

	if !src.Health().PersistFailed {
		t.Fatal("PersistFailed = false after the rotated token could not be saved")
	}
}

func TestFailedSaveKeepsPresentingTheInMemoryRotation(t *testing.T) {
	store := rtsTokenFile(t)
	if err := os.WriteFile(store.Path, []byte("persisted-before-the-disk-filled"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}
	src, stub := rtsSourceWithStore(t, rtsSaveFailingStore{store})
	first, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}

	rejectToken(t, src, first)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if got := stub.gotBody[1].RefreshToken; got != "rotated-refresh-1" {
		t.Fatalf("second exchange presented %q, want the unsaved in-memory rotation", got)
	}
}

func TestSuccessfulSaveClearsPersistFailed(t *testing.T) {
	src, _ := rtsSourceWithStore(t, rtsFirstLoadFailingStore{rtsTokenFile(t), new(atomic.Int64)})
	if !src.Health().PersistFailed {
		t.Fatal("PersistFailed = false although the seed-time read failed")
	}

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	if src.Health().PersistFailed {
		t.Fatal("PersistFailed still set after a successful save")
	}
}

func TestLockFileSitsBesideTheTokenAtOwnerOnlyMode(t *testing.T) {
	store := rtsTokenFile(t)
	src, _ := rtsSourceWithStore(t, store)

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	info, err := os.Stat(store.Path + ".lock")
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
	store := goapihttp.FileRefreshTokenStore{Path: filepath.Join(blocker, "overseer", "readonly_refresh_token")}
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
	if !src.Health().PersistFailed {
		t.Fatal("PersistFailed = false although the token directory cannot be created")
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
	signIns        []pgSignInGrant
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
	var body rtsRefreshGrant
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
	var body pgSignInGrant
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
	rtsWriteJSON(w, map[string]any{
		"access_token":  rtsMakeJWT(rtsAccessMark+"-"+mark, s.clock.now().Add(time.Hour).Unix()),
		"refresh_token": refresh,
	})
}

func (s *pgStub) presentedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.presented...)
}

func (s *pgStub) signInBodies() []pgSignInGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pgSignInGrant(nil), s.signIns...)
}

func pgSource(t *testing.T, stub *pgStub, seed string, opts ...goapihttp.RefreshingOption) *goapihttp.RefreshingTokenSource {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src, err := goapihttp.NewRefreshingTokenSource(srv.URL, "anon-key-SECRET", seed, rtsOptions(srv, stub.clock, opts...)...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
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
	src := pgSource(t, stub, rtsSeedRefresh, goapihttp.WithRefreshTokenStore(store), goapihttp.WithPasswordGrant(pgEmail, pgPassword))

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
	persisted, err := os.ReadFile(store.Path)
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
	src := pgSource(t, stub, rtsSeedRefresh, goapihttp.WithPasswordGrant("", ""))

	var refreshErr *goapihttp.TokenRefreshError
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
			src := pgSource(t, stub, rtsSeedRefresh, goapihttp.WithRefreshTokenStore(rtsTokenFile(t)), goapihttp.WithPasswordGrant(pgEmail, pgPassword))

			_, err := src.Token(context.Background())
			var refreshErr *goapihttp.TokenRefreshError
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

			clock.advance(rtsBackoffMax)
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
	src := pgSource(t, stub, rtsSeedRefresh, goapihttp.WithRefreshTokenStore(rtsTokenFile(t)), goapihttp.WithPasswordGrant(pgEmail, pgPassword))

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
	src := pgSource(t, stub, "", goapihttp.WithRefreshTokenStore(store), goapihttp.WithPasswordGrant(pgEmail, pgPassword))

	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token with credentials and no seed: %v", err)
	}
	if got := stub.refreshCalls.Load(); got != 0 {
		t.Fatalf("refresh exchanges = %d, want 0 when no refresh token is held", got)
	}
	persisted, _ := os.ReadFile(store.Path)
	if string(persisted) != pgSignedIn {
		t.Fatalf("persisted %q, want the signed-in refresh token", persisted)
	}
}

func TestNoSeedAndNoCredentialsStillFailsConstruction(t *testing.T) {
	if _, err := goapihttp.NewRefreshingTokenSource("https://ref.supabase.co", "anon", "", goapihttp.WithPasswordGrant(pgEmail, "")); err == nil {
		t.Fatal("a source with neither a refresh token nor a full credential pair must fail construction")
	}
}

func TestUnreadableTokenFileAtStartupStillServesFromTheEnvSeed(t *testing.T) {
	clock := rtsClock()
	store := rtsTokenFile(t)
	if err := os.WriteFile(store.Path, []byte("cannot-read-me"), 0o000); err != nil {
		t.Fatalf("seed unreadable token file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.Path, 0o600) })
	stub := &rtsRotatingStub{clock: clock}
	stub.respond = func(_ string, n int) (int, string) { return http.StatusOK, fmt.Sprintf("rotated-refresh-%d", n) }
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	src := rtsSourceOnFile(t, srv, clock, store)

	if !src.Health().PersistFailed {
		t.Fatal("PersistFailed = false right after construction although the persisted token file could not be read")
	}

	token, err := src.Token(context.Background())

	if err != nil || token == "" {
		t.Fatalf("Token = %q, %v; want an access token from the env seed", token, err)
	}
	if got := stub.presentedTokens(); len(got) != 1 || got[0] != rtsSeedRefresh {
		t.Fatalf("presented %q, want exactly the env seed", got)
	}
}
