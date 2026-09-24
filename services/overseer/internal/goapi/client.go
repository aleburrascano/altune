package goapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultTimeout bounds every request end to end. go-api being slow or hung must
// never wedge a bucket's collect cycle, so an absent per-request deadline still
// terminates here.
const defaultTimeout = 10 * time.Second

// maxBodyBytes caps how much of a response the client reads. A hostile or
// runaway go-api cannot exhaust Overseer's memory through an unbounded body;
// 1 MiB comfortably covers the JSON reads buckets make.
const maxBodyBytes = 1 << 20

// correlationHeader carries a per-request correlation id to go-api, which adopts
// a well-formed one and echoes it on the response and on the events it emits, so
// a live event, a go-api log line and a failed read for the same request tie
// together. maxCorrelationIDLen and the well-formed charset mirror the contract
// go-api's inbound middleware enforces (internal/shared/httputil): a value it
// rejects it would replace with its own, breaking the correlation.
const (
	correlationHeader   = "X-Correlation-ID"
	maxCorrelationIDLen = 64
)

// newCorrelationID mints a fresh correlation id for one outbound request. Hex
// from a crypto source is well within the length cap and the well-formed charset,
// so go-api adopts it rather than minting its own; it carries no authority, so its
// only requirement is uniqueness, not unguessability. An entropy failure yields
// "" and no header, leaving go-api to mint the id (still echoed, just not known in
// advance) rather than failing the read.
func newCorrelationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// sanitizeCorrID bounds and validates a correlation id arriving from the wire (an
// event's corr_id, or a response's echoed header) against the same contract go-api
// enforces on the way in. A malformed or over-long value — the shape a compromised
// or buggy upstream could inject to forge a log line or grow a buffer — is dropped
// to "" rather than carried into a log or a panel.
func sanitizeCorrID(id string) string {
	if id == "" || len(id) > maxCorrelationIDLen || !isWellFormedCorrID(id) {
		return ""
	}
	return id
}

func isWellFormedCorrID(id string) bool {
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// Client is a read-only HTTP client for go-api's public surface. It exposes GET
// operations only — no method writes, commands or mutates go-api — and attaches
// the read-only bearer token from its TokenSource to every request. It is safe
// for concurrent use.
type Client struct {
	base   *url.URL
	tokens TokenSource
	http   *http.Client
}

// Option customizes a Client at construction.
type Option func(*Client)

// WithHTTPClient supplies the underlying *http.Client (for tests or shared
// transports). A zero Timeout on it is replaced with defaultTimeout so a request
// can never hang forever.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithTimeout sets the end-to-end per-request timeout. A non-positive duration
// is ignored, keeping the default.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.http.Timeout = d
		}
	}
}

// New builds a read-only client against baseURL (e.g. https://api.altune.app),
// authenticating with tokens. It errors on an empty or unparseable baseURL or a
// nil TokenSource, so misconfiguration fails at startup rather than at first
// request.
func New(baseURL string, tokens TokenSource, opts ...Option) (*Client, error) {
	if tokens == nil {
		return nil, errors.New("goapi: nil TokenSource")
	}
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	c := &Client{
		base:   base,
		tokens: tokens,
		http:   &http.Client{Timeout: defaultTimeout, CheckRedirect: refuseRedirect},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.http.Timeout <= 0 {
		c.http.Timeout = defaultTimeout
	}
	return c, nil
}

// parseBaseURL validates and returns the base URL. The parsed *url.URL is kept
// so every request's path is joined onto it structurally (see newRequest): the
// scheme and host are fixed at construction and no per-request path can move the
// request — with its read-only bearer token — to a different host.
func parseBaseURL(baseURL string) (*url.URL, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, errors.New("goapi: empty baseURL")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("goapi: invalid baseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("goapi: baseURL must be http(s), got %q", trimmed)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("goapi: baseURL missing host: %q", trimmed)
	}
	return u, nil
}

// Health is go-api's liveness/readiness response from GET /health.
type Health struct {
	Status string `json:"status"`
}

// OK reports whether go-api considers itself healthy.
func (h Health) OK() bool { return h.Status == "ok" }

// Health fetches GET /health. The endpoint is open, but the client still
// presents the read-only token: a single authenticated read path means the SSE
// leaf and buckets reuse one code route. An unreachable go-api yields a
// SourceDownError.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	if err := c.get(ctx, "/health", &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

// get is the sole read primitive. It performs one bounded GET (getOnce) and, when
// a refreshing token source is in use and go-api answered 401, discards the cached
// token and retries exactly once: a token rejected before its proactive-refresh
// window (early revocation, clock skew) recovers on the retry instead of failing
// the read, while a static source — or any non-401 — takes no retry, so a genuine
// rejection is not amplified into a second request.
func (c *Client) get(ctx context.Context, path string, out any) error {
	op := "GET " + path
	err := c.getOnce(ctx, op, path, out)
	if !c.shouldRefreshRetry(err) {
		return err
	}
	invalidateOn401(c.tokens, http.StatusUnauthorized)
	return c.getOnce(ctx, op, path, out)
}

// shouldRefreshRetry reports whether err is a 401 from go-api AND the token source
// can refresh, the only case a single retry with a fresh token can turn around.
func (c *Client) shouldRefreshRetry(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		return false
	}
	_, ok := c.tokens.(tokenRefresher)
	return ok
}

// getOnce builds a GET for path, attaches the read-only bearer token, executes it,
// maps a transport failure to a SourceDownError and a non-2xx status to an
// APIError, then decodes a bounded body into out. There is no write counterpart,
// by design.
func (c *Client) getOnce(ctx context.Context, op, path string, out any) error {
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}
	sent := req.Header.Get(correlationHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return &SourceDownError{Op: op, Err: err, CorrID: sent}
	}
	if resp == nil {
		return &SourceDownError{Op: op, Err: errors.New("nil response"), CorrID: sent}
	}
	// Bound the drain-for-reuse too: a hostile or runaway body must not be read
	// unboundedly just to free the connection.
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxBodyBytes); _ = resp.Body.Close() }()

	body := io.LimitReader(resp.Body, maxBodyBytes)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(op, resp.StatusCode, echoedCorrID(sent, resp), body)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("goapi: %s: decode response: %w", op, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	// JoinPath appends path as URL path segments onto the fixed base, so the
	// scheme and host cannot be changed by the path — the read-only token can only
	// ever be sent to the configured go-api host.
	reqURL := c.base.JoinPath(path).String()
	return bearerRequest(ctx, c.tokens, reqURL, "application/json")
}

// bearerRequest builds a GET carrying the read-only bearer token from tokens. It
// is the single place request auth is assembled, so the REST client and the SSE
// consumer share one token path (the TokenSource seam) rather than duplicating
// it. A TokenSource error fails closed: no request is built without credentials.
func bearerRequest(ctx context.Context, tokens TokenSource, reqURL, accept string) (*http.Request, error) {
	token, err := tokens.Token(ctx)
	if err != nil {
		return nil, &TokenError{Op: "acquire read-only token", Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("goapi: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	if id := newCorrelationID(); id != "" {
		req.Header.Set(correlationHeader, id)
	}
	return req, nil
}

// refuseRedirect stops every credential-bearing client in this package from
// following a 3xx. The REST client, the refreshing token source and the SSE
// consumer all attach read-only credentials — the Supabase apikey + refresh token,
// or the read-only bearer — so following a redirect would replay those secrets to
// whatever host the 3xx names. A compromised, MITM'd or merely misconfigured
// upstream could otherwise exfiltrate them silently (the Go client re-sends custom
// headers and the body on a same-host redirect, and the header on a cross-host
// one). Returning http.ErrUseLastResponse surfaces the 3xx as the response
// instead, so a credential can only ever reach the configured host — the guarantee
// the JoinPath base already makes for the URL, now held on redirects too. Mirrors
// the security prober's fenced client.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// echoedCorrID is the correlation id go-api reflected on resp, sanitized against
// the wire contract; it falls back to the id we sent when the response echoes none
// (or a malformed one), so an error always names the correlation that failed.
func echoedCorrID(sent string, resp *http.Response) string {
	if echoed := sanitizeCorrID(resp.Header.Get(correlationHeader)); echoed != "" {
		return echoed
	}
	return sent
}

func apiError(op string, status int, corrID string, body io.Reader) error {
	snippet, _ := io.ReadAll(body)
	return &APIError{Op: op, StatusCode: status, CorrID: corrID, Body: strings.TrimSpace(string(snippet))}
}
