package goapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

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
