package probe

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

var ErrOffAllowlist = errors.New("security: target host off allowlist — refused, no request sent")

type sourceDownError struct{ err error }

func (e *sourceDownError) Error() string { return "security: go-api unreachable: " + e.err.Error() }
func (e *sourceDownError) Unwrap() error { return e.err }

type Client struct {
	base      *url.URL
	allowlist map[string]struct{}
	http      *http.Client
}

func New(baseURL string, allow []string) (*Client, error) {
	return NewWithTransport(baseURL, allow, nil)
}

func NewWithTransport(baseURL string, allow []string, transport http.RoundTripper) (*Client, error) {
	base, err := parseBase(baseURL)
	if err != nil {
		return nil, err
	}
	set := hostSet(allow)
	if _, ok := set[normalizeHost(base.Host)]; !ok {
		return nil, fmt.Errorf("security: base host %q not on allowlist %v (fail-closed)", base.Host, allow)
	}
	return &Client{base: base, allowlist: set, http: fencedHTTPClient(transport)}, nil
}

func fencedHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   probeTimeout,
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

func (c *Client) validateTarget(host string) error {
	if _, ok := c.allowlist[normalizeHost(host)]; !ok {
		return fmt.Errorf("%w: %q", ErrOffAllowlist, host)
	}
	return nil
}

func (c *Client) target(path, rawQuery string) *url.URL {
	u := c.base.JoinPath(path)
	u.RawQuery = rawQuery
	return u
}

func (c *Client) Probe(ctx context.Context, path, rawQuery string) (int, error) {
	return c.Do(ctx, c.target(path, rawQuery))
}

func (c *Client) Do(ctx context.Context, target *url.URL) (int, error) {
	if target == nil {
		return 0, errors.New("security: nil probe target")
	}
	if err := c.validateTarget(target.Hostname()); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("security: build probe: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, &sourceDownError{err: err}
	}
	if resp == nil {
		return 0, &sourceDownError{err: errors.New("nil response")}
	}
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxProbeBody); _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
