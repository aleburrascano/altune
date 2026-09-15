package security

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
)

// recordingTransport captures the host of every request that actually reaches
// the transport, so an attack can prove not merely that a request was sent but
// exactly which host it would have connected to.
type recordingTransport struct {
	calls atomic.Int32
	hosts []string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	t.hosts = append(t.hosts, req.URL.Hostname())
	return &http.Response{StatusCode: 401, Body: http.NoBody, Header: make(http.Header)}, nil
}

// TestFenceHostConfusionRefused is the whole-bucket attack on the fence: every
// classic host-confusion trick that could smuggle a probe off the own-infra
// allowlist must be refused BEFORE a socket opens. The allowlist is exactly
// "allowed.test"; each target below is crafted to look adjacent to it.
func TestFenceHostConfusionRefused(t *testing.T) {
	c, err := newFencedClient("https://allowed.test", []string{"allowed.test"})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	rt := &recordingTransport{}
	c.http.Transport = rt

	// Each URL's real connect host (url.Hostname()) is NOT "allowed.test", so
	// the fence must refuse it. The names are chosen to defeat naive substring,
	// suffix, userinfo, and separator checks.
	offAllowlist := []struct {
		name string
		u    *url.URL
	}{
		{"userinfo-smuggle", &url.URL{Scheme: "https", User: url.User("allowed.test"), Host: "evil.example", Path: "/v1/library"}},
		{"userinfo-at-host", mustParse(t, "https://allowed.test@evil.example/v1/library")},
		{"suffix-of-attacker", &url.URL{Scheme: "https", Host: "allowed.test.evil.example", Path: "/"}},
		{"prefix-of-attacker", &url.URL{Scheme: "https", Host: "evilallowed.test", Path: "/"}},
		{"substring-embed", &url.URL{Scheme: "https", Host: "notallowed.test.attacker", Path: "/"}},
		{"trailing-dot", &url.URL{Scheme: "https", Host: "allowed.test.", Path: "/"}},
		{"loopback-ip", &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/"}},
		{"cloud-metadata-ip", &url.URL{Scheme: "http", Host: "169.254.169.254", Path: "/latest/meta-data"}},
		{"ipv6-loopback", &url.URL{Scheme: "http", Host: "[::1]", Path: "/"}},
		{"empty-host", &url.URL{Scheme: "https", Host: "", Path: "/v1/library"}},
	}
	for _, tc := range offAllowlist {
		res := c.do(context.Background(), tc.u)
		if !errors.Is(res.err, errOffAllowlist) {
			t.Errorf("%s: do(%q) err = %v, want errOffAllowlist", tc.name, tc.u.Host, res.err)
		}
	}
	if got := rt.calls.Load(); got != 0 {
		t.Fatalf("fence let %d off-allowlist request(s) reach the transport, hosts=%v", got, rt.hosts)
	}
}

// TestFenceCaseFoldedHostAllowed proves the fence folds case rather than
// deny-by-accident: the SAME owned host in a different case is still the owned
// host and must be allowed, so a legitimate probe is never refused on casing.
func TestFenceCaseFoldedHostAllowed(t *testing.T) {
	c, err := newFencedClient("https://allowed.test", []string{"allowed.test"})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	rt := &recordingTransport{}
	c.http.Transport = rt

	res := c.do(context.Background(), &url.URL{Scheme: "https", Host: "ALLOWED.TEST", Path: "/v1/library"})
	if res.err != nil {
		t.Fatalf("case-folded owned host refused: %v", res.err)
	}
	if rt.calls.Load() != 1 || rt.hosts[0] != "ALLOWED.TEST" {
		t.Errorf("case-folded owned host did not reach transport as expected: calls=%d hosts=%v", rt.calls.Load(), rt.hosts)
	}
}

// TestHostilePathCannotMoveHost is the TOCTOU-style attack: the suite path and
// query are the only attacker-influenced inputs to target(). A path or query
// crafted to look like an authority ("//evil", "@evil") must not move the
// resolved request off the fixed base host — the fence validates the base host,
// and the request must actually connect there, not to a smuggled authority.
func TestHostilePathCannotMoveHost(t *testing.T) {
	hostilePaths := []struct {
		path, query string
	}{
		{"//evil.example/x", ""},
		{"/../..//evil.example", ""},
		{"@evil.example", ""},
		{"/v1/library", "next=https://evil.example/&@evil.example"},
		{"/v1/library", "q=%zz%00#@evil.example"},
	}
	for _, hp := range hostilePaths {
		// A fresh client+transport per case so the recorded host is exactly this
		// probe's connect host, with nothing carried over.
		c, err := newFencedClient("https://allowed.test", []string{"allowed.test"})
		if err != nil {
			t.Fatalf("newFencedClient: %v", err)
		}
		rt := &recordingTransport{}
		c.http.Transport = rt

		res := c.do(context.Background(), c.target(hp.path, hp.query))
		if res.err != nil {
			t.Fatalf("path=%q query=%q errored (expected a clean reach to base): %v", hp.path, hp.query, res.err)
		}
		if len(rt.hosts) != 1 {
			t.Fatalf("path=%q query=%q: recorded %d connect host(s), want exactly 1", hp.path, hp.query, len(rt.hosts))
		}
		if rt.hosts[0] != "allowed.test" {
			t.Errorf("path=%q query=%q connected to %q, want allowed.test — host smuggled", hp.path, hp.query, rt.hosts[0])
		}
	}
}

// TestExplicitOffAllowlistConfigFailsClosed proves an explicit allowlist that
// omits the base host is refused at construction (fail-closed), so a
// misconfiguration cannot silently leave the base host unvalidated.
func TestExplicitOffAllowlistConfigFailsClosed(t *testing.T) {
	if _, err := newFencedClient("https://allowed.test", []string{"other.test", "allowed.test.evil"}); err == nil {
		t.Fatal("newFencedClient accepted an allowlist omitting the base host, want fail-closed")
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
