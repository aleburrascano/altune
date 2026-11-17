package goapihttp

import (
	"altune/overseer/internal/goapi/internal/core"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

const observeLogStreamPath = "/observe/logs/stream"

type logSSEDecoder struct {
	sc   *bufio.Scanner
	data strings.Builder
}

func newLogSSEDecoder(r io.Reader) *logSSEDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxEventBytes)
	return &logSSEDecoder{sc: sc}
}

func (d *logSSEDecoder) next() (core.LogRecord, error) {
	for d.sc.Scan() {
		line := d.sc.Text()
		if line != "" {
			if err := d.accumulate(line); err != nil {
				return core.LogRecord{}, err
			}
			continue
		}
		if rec, ok := d.flush(); ok {
			return rec, nil
		}
	}
	if err := d.sc.Err(); err != nil {
		return core.LogRecord{}, err
	}
	return core.LogRecord{}, io.EOF
}

func (d *logSSEDecoder) accumulate(line string) error {
	name, value := splitField(line)
	if name != "data" {
		return nil
	}
	extra := len(value)
	if d.data.Len() > 0 {
		extra++
	}
	if d.data.Len()+extra > maxEventBytes {
		return ErrFrameTooLarge
	}
	if d.data.Len() > 0 {
		d.data.WriteByte('\n')
	}
	d.data.WriteString(value)
	return nil
}

func (d *logSSEDecoder) flush() (core.LogRecord, bool) {
	raw := d.data.String()
	d.data.Reset()
	if raw == "" {
		return core.LogRecord{}, false
	}
	var rec core.LogRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return core.LogRecord{}, false
	}
	return rec, true
}

type LogsConsumer struct {
	base    *url.URL
	path    string
	tokens  core.TokenSource
	http    *http.Client
	backoff Backoff
	bufSize int
	records *DropOldestQueue[core.LogRecord]

	started atomic.Bool
	health  healthCell
	outage  outage
}

type LogsConsumerOption func(*LogsConsumer)

func WithLogsHTTPClient(h *http.Client) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if h != nil {
			c.http = h
		}
	}
}

func WithLogsBackoff(b Backoff) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if b != nil {
			c.backoff = b
		}
	}
}

func WithLogsBuffer(n int) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if n > 0 {
			c.bufSize = n
		}
	}
}

func WithLogsStreamPath(p string) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if strings.TrimSpace(p) != "" {
			c.path = p
		}
	}
}

func NewLogsConsumer(baseURL string, tokens core.TokenSource, opts ...LogsConsumerOption) (*LogsConsumer, error) {
	if tokens == nil {
		return nil, errors.New("goapi: nil TokenSource")
	}
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	c := &LogsConsumer{
		base:    base,
		path:    observeLogStreamPath,
		tokens:  tokens,
		http:    defaultSSEClient(),
		backoff: newExpBackoff(defaultBackoffBase, defaultBackoffMax),
		bufSize: defaultEventBuffer,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.records = NewDropOldestQueue[core.LogRecord](c.bufSize)
	return c, nil
}

func (c *LogsConsumer) Records() <-chan core.LogRecord { return c.records.pending }

func (c *LogsConsumer) Status() core.Status { return c.health.load().status }

func (c *LogsConsumer) LastError() error {
	return c.health.load().err
}

func (c *LogsConsumer) Health() (core.Status, error) {
	current := c.health.load()
	return current.status, current.err
}

func (c *LogsConsumer) Run(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("goapi: logs consumer already running")
	}
	defer close(c.records.pending)
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

func (c *LogsConsumer) stream(ctx context.Context) bool {
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

func (c *LogsConsumer) pump(ctx context.Context, body io.ReadCloser) {
	watchdog := watchIdle(ctx, body)
	defer watchdog.stop()
	dec := newLogSSEDecoder(watchdog)
	for {
		rec, err := dec.next()
		if err != nil {
			if ctx.Err() == nil {
				c.markDropped(&core.SourceDownError{Op: c.op(), Err: watchdog.cause(err)})
			}
			return
		}
		if !c.emit(ctx, rec) {
			return
		}
	}
}

func (c *LogsConsumer) emit(ctx context.Context, rec core.LogRecord) bool {
	c.records.Push(rec)
	return ctx.Err() == nil
}

func (c *LogsConsumer) Dropped() int { return c.records.Dropped() }

func (c *LogsConsumer) drainPending() []core.LogRecord { return c.records.Drain() }

var _ pendingDrainer[core.LogRecord] = (*LogsConsumer)(nil)

func (c *LogsConsumer) connect(ctx context.Context) (*http.Response, error) {
	reqURL := c.base.JoinPath(c.path).String()
	req, err := bearerRequest(ctx, c.tokens, reqURL, "text/event-stream")
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &core.SourceDownError{Op: c.op(), Err: err}
	}
	if resp == nil {
		return nil, &core.SourceDownError{Op: c.op(), Err: errors.New("nil response")}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.rejectStatus(resp, presentedToken(req))
	}
	return resp, nil
}

func (c *LogsConsumer) rejectStatus(resp *http.Response, presented string) error {
	defer func() { _ = resp.Body.Close() }()
	invalidateOn401(c.tokens, resp.StatusCode, presented)
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return &core.APIError{Op: c.op(), StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
}

func (c *LogsConsumer) wait(ctx context.Context, attempt int) error {
	return sleepThroughOutage(ctx, c.backoff.Backoff(attempt), &c.outage, &c.health)
}

func (c *LogsConsumer) op() string { return "GET " + c.path }

func (c *LogsConsumer) markFailed(err error) {
	c.health.publish(c.outage.status(), err)
}

func (c *LogsConsumer) markDropped(err error) {
	c.health.publish(c.outage.dropped(), err)
}

func (c *LogsConsumer) markUp() {
	c.outage.connected()
	c.health.publish(core.StatusUp, nil)
}
