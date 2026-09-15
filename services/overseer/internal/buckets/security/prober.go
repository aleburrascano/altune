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

// probeTimeout bounds every probe end to end. A slow or hung go-api must never
// wedge the suite; an absent per-request deadline still terminates here.
const probeTimeout = 10 * time.Second

// maxProbeBody caps how much of a probe response is drained for connection
// reuse. A hostile or runaway go-api cannot exhaust Overseer's memory this way.
const maxProbeBody = 1 << 20

// errOffAllowlist is the fence refusal. It is returned BEFORE any socket is
// opened: an off-allowlist target is refused, never sent.
var errOffAllowlist = errors.New("security: target host off allowlist — refused, no request sent")

// errUnconfigured is what the null prober reports when go-api is not configured,
// so an unconfigured bucket degrades to stale instead of failing at startup.
var errUnconfigured = errors.New("security: go-api not configured")

// sourceDown marks a probe that could not reach go-api (a transport failure or
// the unconfigured null prober). It is distinct from a probe that reached the
// app and got an unexpected status, so the bucket can degrade to stale on the
// former without conflating it with a genuine security regression.
type sourceDown struct{ err error }

func (e *sourceDown) Error() string { return "security: go-api unreachable: " + e.err.Error() }
func (e *sourceDown) Unwrap() error { return e.err }

// probeResult is the raw outcome of one fenced probe: the HTTP status if the
// request completed, or an error (a transport failure wrapped as sourceDown, or
// the fence refusal).
type probeResult struct {
	status int
	err    error
}

// prober is the seam the suite drives. Production always uses *fencedClient
// (which fences every request structurally) or nullProber (which sends nothing);
// tests inject doubles. Depending on this interface lets a test drive the suite
// deterministically without weakening the real client's fence.
type prober interface {
	// target resolves a probe path onto the prober's fixed base host. The host is
	// fixed at construction, so a path can never move a probe to another host.
	target(path, rawQuery string) *url.URL
	// do runs one probe. It validates the target host against the allowlist
	// FIRST and refuses (no socket opened) when off-allowlist.
	do(ctx context.Context, target *url.URL) probeResult
}

// fencedClient is the prober's OWN raw HTTP client. Unlike the operator
// goapi.Client it sends UNAUTHENTICATED and deliberately malformed requests, so
// it never carries the operator token. Its crux is the fence: validateTarget
// runs before EVERY request against an own-infra allowlist, and an off-allowlist
// target is refused without a socket being opened. The base URL is fixed at
// construction and every request is a path joined onto it, so a path can never
// move a request to another host; the fence is the structural belt on top.
type fencedClient struct {
	base      *url.URL
	allowlist map[string]struct{}
	http      *http.Client
}

// newFencedClient builds the fenced client against baseURL, allowing only hosts
// on allow. It fails closed: if the base host is not itself on the allowlist the
// client could send nothing, so that misconfiguration is surfaced here rather
// than as a permanently-refused probe at runtime.
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

// fencedHTTPClient builds the underlying client. It refuses to follow redirects:
// a 3xx must never bounce a probe to a host the fence never cleared, so the
// redirect is returned as-is rather than followed.
func fencedHTTPClient() *http.Client {
	return &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// parseBase validates and returns the base URL. Only http(s) with a host is
// accepted, so a malformed base fails at construction, not at first probe.
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

// hostSet normalizes each allowlist entry to a bare lowercase host and collects
// the non-empty ones into a set for O(1) fence checks.
func hostSet(allow []string) map[string]struct{} {
	set := make(map[string]struct{}, len(allow))
	for _, h := range allow {
		if n := normalizeHost(h); n != "" {
			set[n] = struct{}{}
		}
	}
	return set
}

// normalizeHost reduces a full URL, a host:port, or a bare host to a lowercase
// hostname, so the fence compares like with like regardless of how the target
// or allowlist entry was written.
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

// validateTarget is the fence. It reports an error when host is not on the
// own-infra allowlist; do() calls it before opening any socket. It is pure and
// side-effect free, so the fence test can point a probe off-allowlist and assert
// refusal with no request sent.
func (c *fencedClient) validateTarget(host string) error {
	if _, ok := c.allowlist[normalizeHost(host)]; !ok {
		return fmt.Errorf("%w: %q", errOffAllowlist, host)
	}
	return nil
}

// target resolves path (with an optional malformed rawQuery) onto the fixed
// base, joining path as URL segments so it cannot alter the scheme or host.
func (c *fencedClient) target(path, rawQuery string) *url.URL {
	u := c.base.JoinPath(path)
	u.RawQuery = rawQuery
	return u
}

// do runs one fenced probe. The fence is checked FIRST: an off-allowlist target
// returns errOffAllowlist and the http.Client is never touched. Every request is
// a GET carrying no credentials and no body, so no probe can mutate go-api state
// by construction — the suite only reads and asserts rejections.
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
	// Bound the drain-for-reuse: a hostile or runaway body must not be read
	// unboundedly just to free the connection.
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxProbeBody); _ = resp.Body.Close() }()
	return probeResult{status: resp.StatusCode}
}

// nullProber stands in when go-api is unconfigured: every probe reports
// source-down, so the bucket renders stale rather than nil-panicking.
type nullProber struct{}

func (nullProber) target(string, string) *url.URL {
	return &url.URL{Scheme: "null", Host: "unconfigured"}
}

func (nullProber) do(context.Context, *url.URL) probeResult {
	return probeResult{err: &sourceDown{err: errUnconfigured}}
}
