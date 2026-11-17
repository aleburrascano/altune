package probe_test

import (
	"altune/overseer/internal/buckets/security/internal/adapters/probe"
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingTransport struct {
	calls atomic.Int32
	mu    sync.Mutex
	hosts []string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	t.mu.Lock()
	t.hosts = append(t.hosts, req.URL.Hostname())
	t.mu.Unlock()
	return &http.Response{StatusCode: 401, Body: http.NoBody, Header: make(http.Header)}, nil
}

func (t *recordingTransport) seen() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.hosts...)
}

func TestFenceHostConfusionRefused(t *testing.T) {
	rt := &recordingTransport{}
	c := newTestClient(t, rt)

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
		_, err := c.Do(context.Background(), tc.u)
		if !errors.Is(err, probe.ErrOffAllowlist) {
			t.Errorf("%s: Do(%q) err = %v, want ErrOffAllowlist", tc.name, tc.u.Host, err)
		}
	}
	if got := rt.calls.Load(); got != 0 {
		t.Fatalf("fence let %d off-allowlist request(s) reach the transport, hosts=%v", got, rt.seen())
	}
}

func TestFenceCaseFoldedHostAllowed(t *testing.T) {
	rt := &recordingTransport{}
	c := newTestClient(t, rt)

	_, err := c.Do(context.Background(), &url.URL{Scheme: "https", Host: "ALLOWED.TEST", Path: "/v1/library"})
	if err != nil {
		t.Fatalf("case-folded owned host refused: %v", err)
	}
	if hosts := rt.seen(); len(hosts) != 1 || hosts[0] != "ALLOWED.TEST" {
		t.Errorf("case-folded owned host did not reach transport as expected: hosts=%v", hosts)
	}
}

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
		rt := &recordingTransport{}
		c := newTestClient(t, rt)

		if _, err := c.Probe(context.Background(), hp.path, hp.query); err != nil {
			t.Fatalf("path=%q query=%q errored (expected a clean reach to base): %v", hp.path, hp.query, err)
		}
		hosts := rt.seen()
		if len(hosts) != 1 {
			t.Fatalf("path=%q query=%q: recorded %d connect host(s), want exactly 1", hp.path, hp.query, len(hosts))
		}
		if hosts[0] != "allowed.test" {
			t.Errorf("path=%q query=%q connected to %q, want allowed.test — host smuggled", hp.path, hp.query, hosts[0])
		}
	}
}

func TestExplicitOffAllowlistConfigFailsClosed(t *testing.T) {
	if _, err := probe.New("https://allowed.test", []string{"other.test", "allowed.test.evil"}); err == nil {
		t.Fatal("New accepted an allowlist omitting the base host, want fail-closed")
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
