package security

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
)

type recordingTransport struct {
	calls atomic.Int32
	hosts []string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	t.hosts = append(t.hosts, req.URL.Hostname())
	return &http.Response{StatusCode: 401, Body: http.NoBody, Header: make(http.Header)}, nil
}

func TestFenceHostConfusionRefused(t *testing.T) {
	c, err := newFencedClient("https://allowed.test", []string{"allowed.test"})
	if err != nil {
		t.Fatalf("newFencedClient: %v", err)
	}
	rt := &recordingTransport{}
	c.http.Transport = rt

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
