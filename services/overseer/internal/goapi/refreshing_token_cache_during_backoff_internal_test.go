package goapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

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
