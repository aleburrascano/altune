package goapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
