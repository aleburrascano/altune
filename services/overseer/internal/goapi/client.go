package goapi

import (
	"context"
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

// Client is a read-only HTTP client for go-api's public surface. It exposes GET
// operations only — no method writes, commands or mutates go-api — and attaches
// the operator bearer token from its TokenSource to every request. It is safe
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
		http:   &http.Client{Timeout: defaultTimeout},
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
// request — with its operator bearer token — to a different host.
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
// presents the operator token: a single authenticated read path means the SSE
// leaf and buckets reuse one code route. An unreachable go-api yields a
// SourceDownError.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	if err := c.get(ctx, "/health", &out); err != nil {
		return Health{}, err
	}
	return out, nil
}

// get is the sole read primitive: it builds a GET for path, attaches the
// operator bearer token, executes it, maps a transport failure to a
// SourceDownError and a non-2xx status to an APIError, then decodes a bounded
// body into out. There is no write counterpart, by design.
func (c *Client) get(ctx context.Context, path string, out any) error {
	op := "GET " + path
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &SourceDownError{Op: op, Err: err}
	}
	if resp == nil {
		return &SourceDownError{Op: op, Err: errors.New("nil response")}
	}
	// Bound the drain-for-reuse too: a hostile or runaway body must not be read
	// unboundedly just to free the connection.
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxBodyBytes); _ = resp.Body.Close() }()

	body := io.LimitReader(resp.Body, maxBodyBytes)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(op, resp.StatusCode, body)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("goapi: %s: decode response: %w", op, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	// JoinPath appends path as URL path segments onto the fixed base, so the
	// scheme and host cannot be changed by the path — the operator token can only
	// ever be sent to the configured go-api host.
	reqURL := c.base.JoinPath(path).String()
	return bearerRequest(ctx, c.tokens, reqURL, "application/json")
}

// bearerRequest builds a GET carrying the operator bearer token from tokens. It
// is the single place request auth is assembled, so the REST client and the SSE
// consumer share one token path (the TokenSource seam) rather than duplicating
// it. A TokenSource error fails closed: no request is built without credentials.
func bearerRequest(ctx context.Context, tokens TokenSource, reqURL, accept string) (*http.Request, error) {
	token, err := tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("goapi: acquire operator token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("goapi: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	return req, nil
}

func apiError(op string, status int, body io.Reader) error {
	snippet, _ := io.ReadAll(body)
	return &APIError{Op: op, StatusCode: status, Body: strings.TrimSpace(string(snippet))}
}
