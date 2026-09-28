package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type countingTransport struct {
	calls atomic.Int32
	inner http.RoundTripper
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	if t.inner != nil {
		return t.inner.RoundTrip(req)
	}
	return nil, errors.New("no inner transport")
}

func newTestClient(t *testing.T) (*fencedClient, *countingTransport) {
	t.Helper()
	c, err := newFencedClient("https://allowed.test", []string{"allowed.test"})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	ct := &countingTransport{}
	c.http.Transport = ct
	return c, ct
}

func TestFenceRefusesOffAllowlistNoRequestSent(t *testing.T) {
	c, ct := newTestClient(t)

	offTargets := []*url.URL{
		{Scheme: "https", Host: "evil.example", Path: "/v1/library"},
		{Scheme: "http", Host: "169.254.169.254", Path: "/latest/meta-data"},
		{Scheme: "https", Host: "allowed.test.evil.example", Path: "/"},
		{Scheme: "https", Host: "localhost", Path: "/"},
	}
	for _, tgt := range offTargets {
		res := c.do(context.Background(), tgt)
		if !errors.Is(res.err, errOffAllowlist) {
			t.Errorf("do(%s) err = %v, want errOffAllowlist", tgt.Host, res.err)
		}
	}
	if got := ct.calls.Load(); got != 0 {
		t.Fatalf("fence let %d request(s) reach the transport — off-allowlist probe was SENT", got)
	}
}

func TestFenceAllowsAllowlistedHost(t *testing.T) {
	c, _ := newTestClient(t)
	ct := &countingTransport{inner: roundTripStatus(401)}
	c.http.Transport = ct

	res := c.do(context.Background(), c.target("/v1/library", ""))
	if res.err != nil {
		t.Fatalf("allowlisted probe errored: %v", res.err)
	}
	if res.status != 401 {
		t.Errorf("status = %d, want 401", res.status)
	}
	if ct.calls.Load() != 1 {
		t.Errorf("allowlisted probe reached transport %d times, want 1", ct.calls.Load())
	}
}

func TestNewFencedClientFailsClosed(t *testing.T) {
	if _, err := newFencedClient("https://api.test", []string{"other.test"}); err == nil {
		t.Fatal("newFencedClient accepted a base host off its own allowlist, want fail-closed")
	}
}

func TestProbeIssuesGetOnly(t *testing.T) {
	var gotMethod, gotBody atomic.Value
	gotMethod.Store("")
	gotBody.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod.Store(r.Method)
		buf := make([]byte, 8)
		n, _ := r.Body.Read(buf)
		gotBody.Store(string(buf[:n]))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	host := mustHost(t, srv.URL)
	c, err := newFencedClient(srv.URL, []string{host})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	_ = runSuite(context.Background(), c, defaultSuite(), func() time.Time { return time.Unix(0, 0) })

	if m := gotMethod.Load().(string); m != http.MethodGet {
		t.Errorf("probe used method %q, want GET only (no-mutation)", m)
	}
	if b := gotBody.Load().(string); b != "" {
		t.Errorf("probe carried a body %q, want none (no-mutation)", b)
	}
}

func TestFenceBlocksRedirectOffAllowlist(t *testing.T) {
	var followed atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		followed.Store(true)
	}))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, evil.URL+"/pwned", http.StatusFound)
	}))
	defer srv.Close()

	host := mustHost(t, srv.URL)
	c, err := newFencedClient(srv.URL, []string{host})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	res := c.do(context.Background(), c.target("/v1/library", ""))
	if res.err != nil {
		t.Fatalf("probe errored: %v", res.err)
	}
	if res.status != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect returned, not followed)", res.status)
	}
	if followed.Load() {
		t.Fatal("client FOLLOWED a redirect to an off-allowlist host — fence bypassed")
	}
}

func roundTripStatus(status int) http.RoundTripper {
	return roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: http.NoBody, Header: make(http.Header)}, nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}
