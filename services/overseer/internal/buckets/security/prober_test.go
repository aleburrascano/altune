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

// countingTransport records how many requests actually left the client, so a
// test can prove the fence refuses BEFORE a socket is opened.
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

// newTestClient builds a fenced client allowing only "allowed.test" with a
// counting transport wired in, so the fence test can assert no request is sent.
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

// TestFenceRefusesOffAllowlistNoRequestSent is the load-bearing invariant: point
// a probe at a host that is not on the allowlist and it is REFUSED without a
// socket being opened. The counting transport proves the http.Client was never
// touched — the fence is structural, not a post-hoc check on a sent request.
func TestFenceRefusesOffAllowlistNoRequestSent(t *testing.T) {
	c, ct := newTestClient(t)

	offTargets := []*url.URL{
		{Scheme: "https", Host: "evil.example", Path: "/v1/library"},
		// link-local cloud metadata address — a classic SSRF target
		{Scheme: "http", Host: "169.254.169.254", Path: "/latest/meta-data"},
		// suffix trick: an allowlisted name as a subdomain of an attacker host
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

// TestFenceAllowsAllowlistedHost proves the fence is not a blanket deny: a probe
// to the allowlisted base host reaches the transport.
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

// TestNewFencedClientFailsClosed proves a base host absent from its own
// allowlist is rejected at construction, not left to refuse every probe at
// runtime.
func TestNewFencedClientFailsClosed(t *testing.T) {
	if _, err := newFencedClient("https://api.test", []string{"other.test"}); err == nil {
		t.Fatal("newFencedClient accepted a base host off its own allowlist, want fail-closed")
	}
}

// TestProbeIssuesGetOnly is the no-mutation proof: every probe the client sends
// is a GET with no body, so the suite cannot mutate go-api state by
// construction. A recording server captures the actual method and body.
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
	// Run the whole default suite against the recording server.
	_ = runSuite(context.Background(), c, defaultSuite(), func() time.Time { return time.Unix(0, 0) })

	if m := gotMethod.Load().(string); m != http.MethodGet {
		t.Errorf("probe used method %q, want GET only (no-mutation)", m)
	}
	if b := gotBody.Load().(string); b != "" {
		t.Errorf("probe carried a body %q, want none (no-mutation)", b)
	}
}

// TestFenceBlocksRedirectOffAllowlist proves a redirect cannot bounce a probe to
// an off-allowlist host: the client refuses to follow redirects, returning the
// 3xx as-is rather than chasing it to another host.
func TestFenceBlocksRedirectOffAllowlist(t *testing.T) {
	var followed atomic.Bool
	// A server that 302s to an off-allowlist absolute URL. If the client followed
	// it, evil would be hit; CheckRedirect must stop that.
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

// roundTripStatus is a RoundTripper that answers every request with status.
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
