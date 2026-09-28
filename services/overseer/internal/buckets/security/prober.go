package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const probeTimeout = 10 * time.Second

const maxProbeBody = 1 << 20

var errOffAllowlist = errors.New("security: target host off allowlist — refused, no request sent")

var errUnconfigured = errors.New("security: go-api not configured")

type sourceDown struct{ err error }

func (e *sourceDown) Error() string { return "security: go-api unreachable: " + e.err.Error() }
func (e *sourceDown) Unwrap() error { return e.err }

type probeResult struct {
	status int
	err    error
}

type prober interface {
	target(path, rawQuery string) *url.URL
	do(ctx context.Context, target *url.URL) probeResult
}

type fencedClient struct {
	base      *url.URL
	allowlist map[string]struct{}
	http      *http.Client
}

func newFencedClient(baseURL string, allow []string) (*fencedClient, error) {
	base, err := parseBase(baseURL)
	if err != nil {
		return nil, err
	}
	set := hostSet(allow)
	if _, ok := set[normalizeHost(base.Host)]; !ok {
		return nil, fmt.Errorf("security: base host %q not on allowlist %v (fail-closed)", base.Host, allow)
	}
	return &fencedClient{base: base, allowlist: set, http: fencedHTTPClient()}, nil
}

func fencedHTTPClient() *http.Client {
	return &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func parseBase(baseURL string) (*url.URL, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("security: invalid baseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("security: baseURL must be http(s), got %q", trimmed)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("security: baseURL missing host: %q", trimmed)
	}
	return u, nil
}

func hostSet(allow []string) map[string]struct{} {
	set := make(map[string]struct{}, len(allow))
	for _, h := range allow {
		if n := normalizeHost(h); n != "" {
			set[n] = struct{}{}
		}
	}
	return set
}

func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	if strings.Contains(h, "://") {
		if u, err := url.Parse(h); err == nil && u.Host != "" {
			h = u.Host
		}
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(h)
}

func (c *fencedClient) validateTarget(host string) error {
	if _, ok := c.allowlist[normalizeHost(host)]; !ok {
		return fmt.Errorf("%w: %q", errOffAllowlist, host)
	}
	return nil
}

func (c *fencedClient) target(path, rawQuery string) *url.URL {
	u := c.base.JoinPath(path)
	u.RawQuery = rawQuery
	return u
}

func (c *fencedClient) do(ctx context.Context, target *url.URL) probeResult {
	if target == nil {
		return probeResult{err: errors.New("security: nil probe target")}
	}
	if err := c.validateTarget(target.Hostname()); err != nil {
		return probeResult{err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return probeResult{err: fmt.Errorf("security: build probe: %w", err)}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return probeResult{err: &sourceDown{err: err}}
	}
	if resp == nil {
		return probeResult{err: &sourceDown{err: errors.New("nil response")}}
	}
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxProbeBody); _ = resp.Body.Close() }()
	return probeResult{status: resp.StatusCode}
}

type nullProber struct{}

func (nullProber) target(string, string) *url.URL {
	return &url.URL{Scheme: "null", Host: "unconfigured"}
}

func (nullProber) do(context.Context, *url.URL) probeResult {
	return probeResult{err: &sourceDown{err: errUnconfigured}}
}
