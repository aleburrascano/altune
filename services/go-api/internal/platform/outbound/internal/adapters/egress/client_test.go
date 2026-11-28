package egress_test

import (
	"altune/go-api/internal/platform/outbound/internal/adapters/egress"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_RefusesANonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := egress.NewClient(timeout, nil); !errors.Is(err, egress.ErrClientTimeoutRequired) {
			t.Fatalf("timeout %v: err = %v, want ErrClientTimeoutRequired", timeout, err)
		}
	}
}

func TestMustNewClient_PanicsOnANonPositiveTimeout(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewClient(0) did not panic")
		}
	}()

	egress.MustNewClient(0, nil)
}

func TestNewClient_NilTransportKeepsTheNetHTTPDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	client, err := egress.NewClient(time.Second, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = resp.Body.Close()

	if client.Timeout != time.Second || client.Transport != nil {
		t.Fatalf("timeout = %v, transport = %v, want 1s and nil", client.Timeout, client.Transport)
	}
}

func TestNewClient_UsesTheGivenTransport(t *testing.T) {
	errGiven := errors.New("given transport")
	given := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errGiven })
	client, err := egress.NewClient(time.Second, given)
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

func TestDefaultTransport_IsTheNetHTTPDefault(t *testing.T) {
	if egress.DefaultTransport() != http.DefaultTransport {
		t.Fatal("DefaultTransport is not http.DefaultTransport")
	}
}

func TestNewTransport_CarriesEveryOptionAndNothingElse(t *testing.T) {
	o := egress.TransportOptions{
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   11 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   9,
	}

	tr := egress.NewTransport(o)

	if tr.TLSHandshakeTimeout != o.TLSHandshakeTimeout || tr.ResponseHeaderTimeout != o.ResponseHeaderTimeout ||
		tr.IdleConnTimeout != o.IdleConnTimeout || tr.MaxIdleConns != o.MaxIdleConns || tr.MaxIdleConnsPerHost != o.MaxIdleConnsPerHost {
		t.Fatalf("transport fields = %+v, want the options %+v", tr, o)
	}
	if tr.DialContext == nil || tr.Proxy != nil || tr.ForceAttemptHTTP2 || tr.MaxConnsPerHost != 0 {
		t.Fatal("transport has a dialer and no proxy, no HTTP/2 upgrade and no per-host cap, like the literal it replaces")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
