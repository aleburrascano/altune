package goapihttp_test

import (
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCredentialHealthReportsConsecutiveRefreshFailures(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusBadRequest)
	src := rtsNewSwitchSource(t, stub)
	_, _ = src.Token(context.Background())
	clock.advance(rtsBackoffBase + time.Second)
	_, _ = src.Token(context.Background())

	health := src.Health()

	if health.OK() {
		t.Error("a source whose refreshes keep failing reports ok")
	}
	if health.ConsecutiveFailures != 2 {
		t.Errorf("consecutive failures = %d, want 2", health.ConsecutiveFailures)
	}
	if health.LastError != "status 400" {
		t.Errorf("last error = %q, want %q", health.LastError, "status 400")
	}
	if !health.LastRefresh.IsZero() {
		t.Errorf("last refresh = %v, want zero before any success", health.LastRefresh)
	}
}

func TestCredentialHealthRecoversOnASuccessfulRefresh(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	stub.failWith.Store(http.StatusServiceUnavailable)
	src := rtsNewSwitchSource(t, stub)
	_, _ = src.Token(context.Background())
	clock.advance(rtsBackoffBase + time.Second)
	stub.failWith.Store(0)
	refreshedAt := clock.now()
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token after recovery: %v", err)
	}

	health := src.Health()

	if !health.OK() {
		t.Errorf("health = %+v after a successful refresh, want ok", health)
	}
	if health.ConsecutiveFailures != 0 || health.LastError != "" {
		t.Errorf("health = %+v, want the failure state cleared", health)
	}
	if !health.LastRefresh.Equal(refreshedAt) {
		t.Errorf("last refresh = %v, want %v", health.LastRefresh, refreshedAt)
	}
}

func TestCredentialHealthIsNotOKBeforeAnyRefresh(t *testing.T) {
	src := rtsNewSwitchSource(t, &rtsSwitchStub{clock: rtsClock(), lifetime: time.Hour})

	if src.Health().OK() {
		t.Error("a source that has never refreshed reports ok")
	}
}

func TestCredentialHealthReportsPasswordGrantAndPersistFailure(t *testing.T) {
	store := &unreadableStore{}
	src, err := goapihttp.NewRefreshingTokenSource("https://x.supabase.co", "anon-key-SECRET", rtsSeedRefresh,
		goapihttp.WithRefreshTokenStore(store), goapihttp.WithPasswordGrant(pgEmail, pgPassword))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}

	health := src.Health()

	if !health.PasswordGrant {
		t.Error("password grant configured, health says it is not")
	}
	if !health.PersistFailed {
		t.Error("the persisted token could not be read, health says persistence is fine")
	}
}

func TestCredentialHealthCarriesNoSecret(t *testing.T) {
	clock := rtsClock()
	stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
	src := rtsNewSwitchSource(t, stub, goapihttp.WithPasswordGrant(pgEmail, pgPassword))
	access, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	rendered := fmt.Sprintf("%+v", src.Health())

	for _, secret := range []string{access, rtsSeedRefresh, rtsAccessMark, "anon-key-SECRET", pgPassword, pgEmail} {
		if strings.Contains(rendered, secret) {
			t.Errorf("credential health %s leaks %q", rendered, secret)
		}
	}
}

func failedSource(t *testing.T, stage string) *goapihttp.RefreshingTokenSource {
	t.Helper()
	clock := rtsClock()
	switch stage {
	case "status":
		stub := &rtsSwitchStub{clock: clock, lifetime: time.Hour}
		stub.failWith.Store(http.StatusBadRequest)
		return rtsNewSwitchSource(t, stub)
	case "password_grant":
		stub := newPGStub(clock)
		stub.passwordStatus.Store(http.StatusUnauthorized)
		return pgSource(t, stub, rtsSeedRefresh, goapihttp.WithPasswordGrant(pgEmail, pgPassword))
	case "transport":
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		return rtsSourceOn(t, srv.URL, clock)
	default:
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		return rtsSourceOn(t, srv.URL, clock, goapihttp.WithRefreshTokenStore(contendedStore{}))
	}
}

func rtsSourceOn(t *testing.T, endpoint string, clock *rtsFakeClock, opts ...goapihttp.RefreshingOption) *goapihttp.RefreshingTokenSource {
	t.Helper()
	opts = append(opts, goapihttp.WithClock(clock.now))
	src, err := goapihttp.NewRefreshingTokenSource(endpoint, "anon-key-SECRET", rtsSeedRefresh, opts...)
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}
	return src
}

func TestRefreshFailureClassNamesTheStageNeverTheMessage(t *testing.T) {
	cases := []struct {
		stage string
		want  string
	}{
		{"status", "status 400"},
		{"password_grant", "password_grant status 401"},
		{"transport", "transport"},
		{"lock", "lock"},
	}
	for _, tc := range cases {
		t.Run(tc.stage, func(t *testing.T) {
			src := failedSource(t, tc.stage)
			if got := src.Health().LastError; got != "" {
				t.Fatalf("LastError before any refresh = %q, want empty", got)
			}
			if _, err := src.Token(context.Background()); err == nil {
				t.Fatalf("Token succeeded, want a %s failure", tc.stage)
			}
			if got := src.Health().LastError; got != tc.want {
				t.Errorf("LastError = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCredentialHealthReadsSafelyDuringRefresh(t *testing.T) {
	clock := rtsClock()
	src := rtsNewSwitchSource(t, &rtsSwitchStub{clock: clock, lifetime: time.Hour})
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = src.Token(context.Background())
		}()
		go func() {
			defer wg.Done()
			_ = src.Health()
		}()
	}
	wg.Wait()

	if !src.Health().OK() {
		t.Errorf("health = %+v after concurrent refreshes, want ok", src.Health())
	}
}

type unreadableStore struct{}

func (*unreadableStore) Lock(context.Context) (func(), error) { return func() {}, nil }
func (*unreadableStore) Load() (string, error)                { return "", errors.New("permission denied") }
func (*unreadableStore) Save(string) error                    { return nil }

type contendedStore struct{}

func (contendedStore) Lock(context.Context) (func(), error) { return nil, context.DeadlineExceeded }
func (contendedStore) Load() (string, error)                { return "", nil }
func (contendedStore) Save(string) error                    { return nil }
