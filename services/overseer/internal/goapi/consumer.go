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

const observeEventStreamPath = "/observe/events/stream"

const defaultEventBuffer = 256

const connectTimeout = 10 * time.Second

type Status int32

const (
	StatusConnecting Status = iota
	StatusUp
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

type ConsumerOption func(*Consumer)

func WithConsumerHTTPClient(h *http.Client) ConsumerOption {
	return func(c *Consumer) {
		if h != nil {
			c.http = h
		}
	}
}

func WithBackoff(b Backoff) ConsumerOption {
	return func(c *Consumer) {
		if b != nil {
			c.backoff = b
		}
	}
}

func WithEventBuffer(n int) ConsumerOption {
	return func(c *Consumer) {
		if n > 0 {
			c.bufSize = n
		}
	}
}

func WithStreamPath(p string) ConsumerOption {
	return func(c *Consumer) {
		if strings.TrimSpace(p) != "" {
			c.path = p
		}
	}
}

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

func (c *Consumer) Events() <-chan Event { return c.events.pending }

func (c *Consumer) Status() Status { return c.health.load().status }

func (c *Consumer) LastError() error {
	return c.health.load().err
}

func (c *Consumer) Health() (Status, error) {
	current := c.health.load()
	return current.status, current.err
}

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
			attempt = 0
		}
		attempt++
		if err := c.wait(ctx, attempt); err != nil {
			return err
		}
	}
}

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

func (c *Consumer) rejectStatus(resp *http.Response, presented string) error {
	defer func() { _ = resp.Body.Close() }()
	invalidateOn401(c.tokens, resp.StatusCode, presented)
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return &APIError{Op: c.op(), StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
}

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
