package probe_test

import (
	"altune/overseer/internal/buckets/security/internal/adapters/probe"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
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

func newTestClient(t *testing.T, transport http.RoundTripper) *probe.Client {
	t.Helper()
	c, err := probe.NewWithTransport("https://allowed.test", []string{"allowed.test"}, transport)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	return c
}

func TestFenceRefusesOffAllowlistNoRequestSent(t *testing.T) {
	ct := &countingTransport{}
	c := newTestClient(t, ct)

	offTargets := []*url.URL{
		{Scheme: "https", Host: "evil.example", Path: "/v1/library"},
		{Scheme: "http", Host: "169.254.169.254", Path: "/latest/meta-data"},
		{Scheme: "https", Host: "allowed.test.evil.example", Path: "/"},
		{Scheme: "https", Host: "localhost", Path: "/"},
	}
	for _, tgt := range offTargets {
		_, err := c.Do(context.Background(), tgt)
		if !errors.Is(err, probe.ErrOffAllowlist) {
			t.Errorf("Do(%s) err = %v, want ErrOffAllowlist", tgt.Host, err)
		}
	}
	if got := ct.calls.Load(); got != 0 {
		t.Fatalf("fence let %d request(s) reach the transport — off-allowlist probe was SENT", got)
	}
}

func TestFenceRefusesANilTarget(t *testing.T) {
	ct := &countingTransport{}
	c := newTestClient(t, ct)

	if _, err := c.Do(context.Background(), nil); err == nil {
		t.Fatal("Do(nil) returned no error, want a refusal")
	}
	if got := ct.calls.Load(); got != 0 {
		t.Fatalf("a nil target reached the transport %d time(s)", got)
	}
}

func TestFenceAllowsAllowlistedHost(t *testing.T) {
	ct := &countingTransport{inner: roundTripStatus(401)}
	c := newTestClient(t, ct)

	status, err := c.Probe(context.Background(), "/v1/library", "")
	if err != nil {
		t.Fatalf("allowlisted probe errored: %v", err)
	}
	if status != 401 {
		t.Errorf("status = %d, want 401", status)
	}
	if ct.calls.Load() != 1 {
		t.Errorf("allowlisted probe reached transport %d times, want 1", ct.calls.Load())
	}
}

func TestAnUnreachableGoAPIIsReportedAsSourceDown(t *testing.T) {
	c := newTestClient(t, &countingTransport{})

	_, err := c.Probe(context.Background(), "/v1/library", "")
	if err == nil || !strings.HasPrefix(err.Error(), "security: go-api unreachable: ") {
		t.Fatalf("transport failure err = %v, want the go-api unreachable error", err)
	}
	if errors.Is(err, probe.ErrOffAllowlist) {
		t.Fatalf("transport failure reported as an allowlist refusal: %v", err)
	}
}

func TestNewFencedClientFailsClosed(t *testing.T) {
	if _, err := probe.New("https://api.test", []string{"other.test"}); err == nil {
		t.Fatal("New accepted a base host off its own allowlist, want fail-closed")
	}
}

func TestNewRefusesABaseURLItCannotProbe(t *testing.T) {
	for _, base := range []string{"://not a url", "ftp://api.test", "https://", ""} {
		if _, err := probe.New(base, []string{"api.test"}); err == nil {
			t.Errorf("New(%q) accepted a base it cannot probe", base)
		}
	}
}

func TestNewAcceptsAnAllowlistOfURLsAndPorts(t *testing.T) {
	for _, allow := range [][]string{
		{"https://API.test:8443/"},
		{" api.test:8443 "},
		{"", "api.test"},
	} {
		if _, err := probe.New(" https://api.test:8443/ ", allow); err != nil {
			t.Errorf("New with allowlist %q refused the base: %v", allow, err)
		}
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

	c, err := probe.New(srv.URL, []string{mustHost(t, srv.URL)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := c.Probe(context.Background(), "/v1/discovery/search", "q=overseer-selftest")
	if err != nil || status != http.StatusUnauthorized {
		t.Fatalf("probe = %d, %v, want 401", status, err)
	}

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

	c, err := probe.New(srv.URL, []string{mustHost(t, srv.URL)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := c.Probe(context.Background(), "/v1/library", "")
	if err != nil {
		t.Fatalf("probe errored: %v", err)
	}
	if status != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect returned, not followed)", status)
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
