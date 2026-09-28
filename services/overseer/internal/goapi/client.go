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

const defaultTimeout = 10 * time.Second

const maxBodyBytes = 1 << 20

const (
	correlationHeader   = "X-Correlation-ID"
	maxCorrelationIDLen = 64
)

func newCorrelationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

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

type Client struct {
	base   *url.URL
	tokens TokenSource
	http   *http.Client
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.http.Timeout = d
		}
	}
}

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

type Health struct {
	Status string `json:"status"`
}

const healthStatusDegraded = "degraded"

func (h Health) OK() bool { return h.Status == "ok" }

func (h Health) Degraded() bool { return h.Status == healthStatusDegraded }

func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	if err := c.getPublicOnce(ctx, "/health", &out, http.StatusServiceUnavailable); err != nil {
		return Health{}, err
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, out any, readableStatus ...int) error {
	op := "GET " + path
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}
	err = c.doRead(op, req, out, readableStatus)
	if !c.shouldRefreshRetry(err) {
		return err
	}
	invalidateOn401(c.tokens, http.StatusUnauthorized, presentedToken(req))
	return c.getOnce(ctx, op, path, out, readableStatus...)
}

func (c *Client) shouldRefreshRetry(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		return false
	}
	_, ok := c.tokens.(tokenRefresher)
	return ok
}

func (c *Client) getOnce(ctx context.Context, op, path string, out any, readableStatus ...int) error {
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}
	return c.doRead(op, req, out, readableStatus)
}

func (c *Client) getPublicOnce(ctx context.Context, path string, out any, readableStatus ...int) error {
	op := "GET " + path
	req, err := c.newPublicRequest(ctx, path)
	if err != nil {
		return err
	}
	return c.doRead(op, req, out, readableStatus)
}

func (c *Client) doRead(op string, req *http.Request, out any, readableStatus []int) error {
	sent := req.Header.Get(correlationHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return &SourceDownError{Op: op, Err: err, CorrID: sent}
	}
	if resp == nil {
		return &SourceDownError{Op: op, Err: errors.New("nil response"), CorrID: sent}
	}
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxBodyBytes); _ = resp.Body.Close() }()

	body := io.LimitReader(resp.Body, maxBodyBytes)
	if !isOKStatus(resp.StatusCode, readableStatus) {
		return apiError(op, resp.StatusCode, echoedCorrID(sent, resp), body)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("goapi: %s: decode response: %w", op, err)
	}
	return nil
}

func isOKStatus(status int, readableStatus []int) bool {
	if status >= 200 && status < 300 {
		return true
	}
	for _, s := range readableStatus {
		if status == s {
			return true
		}
	}
	return false
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	reqURL := c.base.JoinPath(path).String()
	return bearerRequest(ctx, c.tokens, reqURL, "application/json")
}

func (c *Client) newPublicRequest(ctx context.Context, path string) (*http.Request, error) {
	reqURL := c.base.JoinPath(path).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("goapi: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if id := newCorrelationID(); id != "" {
		req.Header.Set(correlationHeader, id)
	}
	return req, nil
}

func bearerRequest(ctx context.Context, tokens TokenSource, reqURL, accept string) (*http.Request, error) {
	token, err := tokens.Token(ctx)
	if err != nil {
		return nil, &TokenError{Op: "acquire read-only token", Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("goapi: build request: %w", err)
	}
	req.Header.Set("Authorization", bearerPrefix+token)
	req.Header.Set("Accept", accept)
	if id := newCorrelationID(); id != "" {
		req.Header.Set(correlationHeader, id)
	}
	return req, nil
}

const bearerPrefix = "Bearer "

func presentedToken(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), bearerPrefix)
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

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
