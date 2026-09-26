package goapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// observeEventStreamPath is go-api's operator SSE endpoint
// (internal/observe/handler/streams.go mounts "/events/stream" under
// "/observe"). It emits the system-wide event tap the in-process consumer cannot
// share, which is why Overseer consumes it out of process.
const observeEventStreamPath = "/observe/events/stream"

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

const ReasonConnecting = "connecting"

func (s Status) PanelReason(failureReason string) string {
	switch s {
	case StatusUp:
		return ""
	case StatusConnecting:
		return ReasonConnecting
	default:
		return failureReason
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
	events  dropOldestQueue[Event]

	started atomic.Bool
	health  healthCell
	outage  outage
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
		path:    observeEventStreamPath,
		tokens:  tokens,
		http:    defaultSSEClient(),
		backoff: NewExpBackoff(defaultBackoffBase, defaultBackoffMax),
		bufSize: defaultEventBuffer,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.events.pending = make(chan Event, c.bufSize)
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
func (c *Consumer) Events() <-chan Event { return c.events.pending }

// Status returns the current connection state.
func (c *Consumer) Status() Status { return c.health.load().status }

// LastError returns the most recent connection failure (a *SourceDownError for
// an unreachable upstream, a *APIError for a non-2xx such as a 502 during a
// deploy), or nil while healthy. Callers branch with IsSourceDown.
func (c *Consumer) LastError() error {
	return c.health.load().err
}

func (c *Consumer) Health() (Status, error) {
	current := c.health.load()
	return current.status, current.err
}

// Run streams events until ctx is cancelled. It connects, emits decoded events on
// Events(), and on any disconnect (go-api restart, network blip, hung connect)
// waits a backoff interval and reconnects — resuming the stream. It returns
// ctx.Err() on shutdown and closes Events(). Run may be called at most once per
// Consumer.
func (c *Consumer) Run(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("goapi: consumer already running")
	}
	defer close(c.events.pending)
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
// established (a 200 body), so Run resets backoff only after real progress.
func (c *Consumer) stream(ctx context.Context) bool {
	resp, err := c.connect(ctx)
	if err != nil {
		c.markFailed(err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	c.markUp()
	c.pump(ctx, resp.Body)
	return true
}

// pump reads events off body until the stream ends or ctx is cancelled, sending
// each on the events channel.
func (c *Consumer) pump(ctx context.Context, body io.ReadCloser) {
	watchdog := watchIdle(ctx, body)
	defer watchdog.stop()
	dec := newSSEDecoder(watchdog)
	for {
		ev, err := dec.next()
		if err != nil {
			if ctx.Err() == nil {
				c.markDropped(&SourceDownError{Op: c.op(), Err: watchdog.cause(err)})
			}
			return
		}
		if !c.emit(ctx, ev) {
			return
		}
	}
}

func (c *Consumer) emit(ctx context.Context, ev Event) bool {
	c.events.push(ev)
	return ctx.Err() == nil
}

func (c *Consumer) Dropped() int { return c.events.dropped() }

func (c *Consumer) drainPending() []Event { return c.events.drain() }

var _ pendingDrainer[Event] = (*Consumer)(nil)

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
		return nil, c.rejectStatus(resp, presentedToken(req))
	}
	return resp, nil
}

// rejectStatus drains a bounded snippet, closes the body, and returns the typed
// APIError for a non-2xx response. On a 401 it discards a refreshing source's
// cached token so the next reconnect presents a fresh one rather than re-offering
// the token go-api just refused.
func (c *Consumer) rejectStatus(resp *http.Response, presented string) error {
	defer func() { _ = resp.Body.Close() }()
	invalidateOn401(c.tokens, resp.StatusCode, presented)
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return &APIError{Op: c.op(), StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
}

// wait blocks for the backoff interval before attempt n, returning early with
// ctx.Err() if the consumer is shut down mid-wait — so a pending backoff never
// delays a clean shutdown and its timer never leaks.
func (c *Consumer) wait(ctx context.Context, attempt int) error {
	return sleepThroughOutage(ctx, c.backoff.Backoff(attempt), &c.outage, &c.health)
}

func (c *Consumer) op() string { return "GET " + c.path }

func (c *Consumer) markFailed(err error) {
	c.health.publish(c.outage.status(), err)
}

func (c *Consumer) markDropped(err error) {
	c.health.publish(c.outage.dropped(), err)
}

func (c *Consumer) markUp() {
	c.outage.connected()
	c.health.publish(StatusUp, nil)
}
