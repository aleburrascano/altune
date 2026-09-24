package goapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	clock.advance(refreshBackoffBase + time.Second)
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
	clock.advance(refreshBackoffBase + time.Second)
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
	src, err := NewRefreshingTokenSource("https://x.supabase.co", "anon-key-SECRET", rtsSeedRefresh,
		WithRefreshTokenStore(store), WithPasswordGrant(pgEmail, pgPassword))
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
	src := rtsNewSwitchSource(t, stub)
	WithPasswordGrant(pgEmail, pgPassword)(src)
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

func TestRefreshFailureClassNamesTheStageNeverTheMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no failure", nil, ""},
		{"token endpoint status", &TokenRefreshError{Stage: "status", Status: 400, Err: errRefreshRejected}, "status 400"},
		{"password grant status", &TokenRefreshError{Stage: "password_grant", Status: 401, Err: errRefreshRejected}, "password_grant status 401"},
		{"transport", &TokenRefreshError{Stage: "transport", Err: errors.New("dial tcp secret-host: refused")}, "transport"},
		{"lock", &TokenRefreshError{Stage: "lock", Err: errStoreLockUnavailable}, "lock"},
		{"foreign error", errors.New("SECRET-bearing message"), unclassifiedRefreshFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := refreshFailureClass(tc.err); got != tc.want {
				t.Errorf("refreshFailureClass = %q, want %q", got, tc.want)
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

func (*unreadableStore) lock(context.Context) (func(), error) { return func() {}, nil }
func (*unreadableStore) load() (string, error)                { return "", errors.New("permission denied") }
func (*unreadableStore) save(string) error                    { return nil }
