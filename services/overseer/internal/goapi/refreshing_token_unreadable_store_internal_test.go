package goapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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
