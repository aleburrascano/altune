package goapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// operatorEventStreamPath is go-api's operator SSE endpoint
// (internal/admin/handler/admin_handler.go mounts "/events/stream" under
// "/admin"). It emits the system-wide event tap the in-process consumer cannot
// share, which is why Overseer consumes it out of process.
const operatorEventStreamPath = "/admin/events/stream"

// defaultEventBuffer bounds the events channel. It absorbs bursts without
// blocking the reader, and — being fixed — caps memory: a slow bucket applies
// backpressure (the pump blocks on a full buffer) rather than letting the buffer
// grow without bound.
const defaultEventBuffer = 256

// connectTimeout bounds the connect/handshake and response-header wait. It does
// NOT bound the live stream (that must last indefinitely), so a hung go-api
// surfaces as source-down promptly while a healthy stream is never cut short.
const connectTimeout = 10 * time.Second

// Status is the consumer's current connection state to go-api's event stream.
// Buckets read it to render "live" versus "stale / source down" without
// inspecting errors: StatusDown is the typed source-down signal the
// outlives-the-app spine requires.
type Status int32

const (
	// StatusConnecting is the initial state and the state between a drop and the
	// next successful connection.
	StatusConnecting Status = iota
	// StatusUp means a stream is established and events are flowing.
	StatusUp
	// StatusDown means the upstream is unreachable; last-known state is stale.
	StatusDown
)

func (s Status) String() string {
	switch s {
	case StatusUp:
		return "up"
	case StatusDown:
		return "down"
	case StatusConnecting:
		return "connecting"
	default:
		return "unknown"
	}
}

// PanelState maps the connection status onto the frontend's three-state panel
// vocabulary ("live" | "stale" | "source_down"), the single place SSE-backed
// buckets translate a Status into the core.State the JSON API reports. It returns
// a bare string rather than a core.State so goapi stays free of a core import; the
// caller wraps it with core.State(...). Up is live, an unreachable upstream is
// source_down, and connecting (initial or between a drop and reconnect) is stale.
func (s Status) PanelState() string {
	switch s {
	case StatusUp:
		return "live"
	case StatusDown:
		return "source_down"
	case StatusConnecting:
		return "stale"
	default:
		return "stale"
	}
}

// Consumer streams go-api's operator event SSE from outside the process. It
// reuses the REST client's read-only auth (the TokenSource seam — no second token
// path), yields decoded events on a channel, reconnects with backoff across
// go-api restarts, and exposes a typed source-down Status so buckets degrade
// instead of crashing. A Consumer runs once; construct another to run again.
type Consumer struct {
	base    *url.URL
	path    string
	tokens  TokenSource
	http    *http.Client
	backoff Backoff
	bufSize int
	events  chan Event

	status  atomic.Int32
	started atomic.Bool

	mu      sync.Mutex
	lastErr error
}

// ConsumerOption customizes a Consumer at construction.
type ConsumerOption func(*Consumer)

// WithConsumerHTTPClient supplies the underlying *http.Client (tests or a shared
// transport). A nil client is ignored. The stream is long-lived, so a client
// with a non-zero Timeout would sever a healthy stream — prefer the default.
func WithConsumerHTTPClient(h *http.Client) ConsumerOption {
	return func(c *Consumer) {
		if h != nil {
			c.http = h
		}
	}
}

// WithBackoff sets the reconnect backoff policy. A nil policy is ignored.
func WithBackoff(b Backoff) ConsumerOption {
	return func(c *Consumer) {
		if b != nil {
			c.backoff = b
		}
	}
}

// WithEventBuffer sets the events channel capacity. A non-positive value is
// ignored, keeping the default bound.
func WithEventBuffer(n int) ConsumerOption {
	return func(c *Consumer) {
		if n > 0 {
			c.bufSize = n
		}
	}
}

// WithStreamPath overrides the SSE path (defaults to the operator event stream).
// A blank path is ignored.
func WithStreamPath(p string) ConsumerOption {
	return func(c *Consumer) {
		if strings.TrimSpace(p) != "" {
			c.path = p
		}
	}
}

// NewConsumer builds an SSE consumer against baseURL, authenticating with tokens.
// It errors on an empty or unparseable baseURL or a nil TokenSource, so
// misconfiguration fails at startup rather than at first connect.
func NewConsumer(baseURL string, tokens TokenSource, opts ...ConsumerOption) (*Consumer, error) {
	if tokens == nil {
		return nil, errors.New("goapi: nil TokenSource")
	}
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	c := &Consumer{
		base:    base,
		path:    operatorEventStreamPath,
		tokens:  tokens,
		http:    defaultSSEClient(),
		backoff: NewExpBackoff(defaultBackoffBase, defaultBackoffMax),
		bufSize: defaultEventBuffer,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.events = make(chan Event, c.bufSize)
	return c, nil
}

// defaultSSEClient bounds connect/handshake and response-header waits but never
// the stream body, so a hung upstream is detected quickly while a live stream
// runs indefinitely.
func defaultSSEClient() *http.Client {
	return &http.Client{
		Timeout:       0,
		CheckRedirect: refuseRedirect,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: connectTimeout,
		},
	}
}

// Events is the receive-only channel of decoded events. Run closes it on exit,
// so a `range` over it terminates cleanly on shutdown.
func (c *Consumer) Events() <-chan Event { return c.events }

// Status returns the current connection state.
func (c *Consumer) Status() Status { return Status(c.status.Load()) }

// LastError returns the most recent connection failure (a *SourceDownError for
// an unreachable upstream, a *APIError for a non-2xx such as a 502 during a
// deploy), or nil while healthy. Callers branch with IsSourceDown.
func (c *Consumer) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Run streams events until ctx is cancelled. It connects, emits decoded events on
// Events(), and on any disconnect (go-api restart, network blip, hung connect)
// marks the status down, waits a backoff interval, and reconnects — resuming the
// stream. It returns ctx.Err() on shutdown and closes Events(). Run may be called
// at most once per Consumer.
func (c *Consumer) Run(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("goapi: consumer already running")
	}
	defer close(c.events)
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.stream(ctx) {
			attempt = 0 // a real session resets the backoff
		}
		attempt++
		if err := c.wait(ctx, attempt); err != nil {
			return err
		}
	}
}

// stream runs one connection attempt. It returns whether a stream was actually
// established (a 200 body), so Run resets backoff only after real progress. Every
// failure path records a typed error and flips the status down.
func (c *Consumer) stream(ctx context.Context) bool {
	resp, err := c.connect(ctx)
	if err != nil {
		c.markDown(err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	c.markUp()
	c.pump(ctx, resp.Body)
	return true
}

// pump reads events off body until the stream ends or ctx is cancelled, sending
// each on the events channel. A watcher closes body on ctx cancellation so a
// blocked read unblocks promptly (no goroutine leak on shutdown); a read error
// that is not a clean shutdown flips the status down.
func (c *Consumer) pump(ctx context.Context, body io.ReadCloser) {
	stop := c.closeOnDone(ctx, body)
	defer stop()
	dec := newSSEDecoder(body)
	for {
		ev, err := dec.next()
		if err != nil {
			if ctx.Err() == nil {
				c.markDown(&SourceDownError{Op: c.op(), Err: err})
			}
			return
		}
		if !c.emit(ctx, ev) {
			return
		}
	}
}

// emit sends ev on the events channel, abandoning the send if ctx is cancelled so
// shutdown never blocks on a full buffer with no reader. A full buffer otherwise
// applies backpressure (bounded memory). It returns false when ctx is done.
func (c *Consumer) emit(ctx context.Context, ev Event) bool {
	select {
	case c.events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// closeOnDone closes closer on ctx cancellation and returns a stop func that
// tears the watcher down deterministically when the stream ends on its own, so
// no goroutine leaks per reconnect.
func (c *Consumer) closeOnDone(ctx context.Context, closer io.Closer) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// connect issues the SSE GET with the read-only bearer token. A transport failure
// becomes a *SourceDownError; a non-2xx becomes a *APIError (go-api answered —
// e.g. 401 rejected, or 502 while a deploy swaps). The caller owns closing the
// body on success.
func (c *Consumer) connect(ctx context.Context) (*http.Response, error) {
	reqURL := c.base.JoinPath(c.path).String()
	req, err := bearerRequest(ctx, c.tokens, reqURL, "text/event-stream")
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &SourceDownError{Op: c.op(), Err: err}
	}
	if resp == nil {
		return nil, &SourceDownError{Op: c.op(), Err: errors.New("nil response")}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.rejectStatus(resp)
	}
	return resp, nil
}

// rejectStatus drains a bounded snippet, closes the body, and returns the typed
// APIError for a non-2xx response. On a 401 it discards a refreshing source's
// cached token so the next reconnect presents a fresh one rather than re-offering
// the token go-api just refused.
func (c *Consumer) rejectStatus(resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	invalidateOn401(c.tokens, resp.StatusCode)
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return &APIError{Op: c.op(), StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
}

// wait blocks for the backoff interval before attempt n, returning early with
// ctx.Err() if the consumer is shut down mid-wait — so a pending backoff never
// delays a clean shutdown and its timer never leaks.
func (c *Consumer) wait(ctx context.Context, attempt int) error {
	d := c.backoff.Backoff(attempt)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Consumer) op() string { return "GET " + c.path }

func (c *Consumer) setStatus(s Status) { c.status.Store(int32(s)) }

// markDown records the failure and flips the status down in one place, so the
// source-down state and its typed cause never disagree.
func (c *Consumer) markDown(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.setStatus(StatusDown)
}

func (c *Consumer) markUp() {
	c.mu.Lock()
	c.lastErr = nil
	c.mu.Unlock()
	c.setStatus(StatusUp)
}
