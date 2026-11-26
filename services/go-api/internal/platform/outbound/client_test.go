package outbound_test

import (
	"altune/go-api/internal/platform/outbound"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_RefusesANonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := outbound.NewClient(timeout, nil); !errors.Is(err, outbound.ErrClientTimeoutRequired) {
			t.Fatalf("timeout %v: err = %v, want ErrClientTimeoutRequired", timeout, err)
		}
	}
}

func TestNewClient_DefaultTransportRefusesNonPublicAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	client, err := outbound.NewClient(time.Second, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Timeout != time.Second {
		t.Fatalf("timeout = %v, want 1s", client.Timeout)
	}

	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, outbound.ErrNonPublicAddr) {
		t.Fatalf("err = %v, want ErrNonPublicAddr", err)
	}
}

func TestNewClient_UsesTheGivenTransport(t *testing.T) {
	errGiven := errors.New("given transport")
	given := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errGiven })
	client, err := outbound.NewClient(time.Second, given)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Get("http://example.invalid/")
	if err == nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, errGiven) {
		t.Fatalf("err = %v, want the given transport's error", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
