package goapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// operatorLogStreamPath is go-api's operator log SSE endpoint
// (internal/admin/handler/admin_handler.go mounts "/logs/stream" under "/admin";
// the handler is streamLogs over the in-process log ring). It is the second SSE
// stream Overseer consumes — the events Consumer targets /admin/events/stream and
// stays untouched; this leaf reuses the frame decoder and reconnect typing rather
// than generalizing that consumer.
const operatorLogStreamPath = "/admin/logs/stream"

// LogRecord is one structured log line decoded from go-api's log SSE stream. Its
// fields mirror go-api's wire record (internal/shared/logging.CapturedRecord):
// the timestamp, the level, the message, and a bag of string attributes. Every
// one of these is watched-app data — the bucket HTML-escapes each before it ever
// reaches a panel.
type LogRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"attrs,omitempty"`
}

// logSSEDecoder reads go-api's text/event-stream log body one frame at a time. It
// reuses sse.go's frame grammar — the same splitField parser, the same
// maxEventBytes cap on both a single line and the accumulated multi-line frame,
// and the same errFrameTooLarge signal the pump treats as a dropped stream — but
// flushes each frame into a LogRecord instead of an Event. Sharing the events
// decoder's grammar without editing it keeps the two streams disjoint (the design
// decision) while a hostile or runaway upstream still cannot OOM Overseer.
type logSSEDecoder struct {
	sc   *bufio.Scanner
	data strings.Builder
}

func newLogSSEDecoder(r io.Reader) *logSSEDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxEventBytes)
	return &logSSEDecoder{sc: sc}
}

// next returns the next decoded log record, io.EOF when the stream ends cleanly,
// or a read/scan error (including an over-long frame). A malformed JSON frame is
// skipped rather than tearing down the stream — one bad record must not cost the
// resume.
func (d *logSSEDecoder) next() (LogRecord, error) {
	for d.sc.Scan() {
		line := d.sc.Text()
		if line != "" {
			if err := d.accumulate(line); err != nil {
				return LogRecord{}, err
			}
			continue
		}
		if rec, ok := d.flush(); ok {
			return rec, nil
		}
	}
	if err := d.sc.Err(); err != nil {
		return LogRecord{}, err
	}
	return LogRecord{}, io.EOF
}

// accumulate folds one non-blank line into the pending frame, keeping only the
// data field and bounding the accumulated frame at maxEventBytes so a multi-line
// frame that never terminates cannot grow the buffer without bound.
func (d *logSSEDecoder) accumulate(line string) error {
	name, value := splitField(line)
	if name != "data" {
		return nil
	}
	extra := len(value)
	if d.data.Len() > 0 {
		extra++ // the '\n' separator between folded data lines
	}
	if d.data.Len()+extra > maxEventBytes {
		return errFrameTooLarge
	}
	if d.data.Len() > 0 {
		d.data.WriteByte('\n')
	}
	d.data.WriteString(value)
	return nil
}

// flush decodes and clears the pending frame. ok is false for an empty or
// malformed frame, so the caller keeps reading instead of emitting a zero record.
func (d *logSSEDecoder) flush() (LogRecord, bool) {
	raw := d.data.String()
	d.data.Reset()
	if raw == "" {
		return LogRecord{}, false
	}
	var rec LogRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return LogRecord{}, false
	}
	return rec, true
}

// LogsConsumer streams go-api's operator log SSE from outside the process. It is
// the events Consumer's sibling for the second stream: same read-only auth
// (TokenSource), same reconnect-with-backoff across go-api restarts, same typed
// source-down Status buckets read to degrade instead of crash — but it decodes
// LogRecords off /admin/logs/stream and yields them on its own channel. A
// LogsConsumer runs once; construct another to run again. Kept a separate type
// rather than generalizing Consumer so this epic never touches the events
// consumer (the design decision; factor a shared core if a third stream appears).
type LogsConsumer struct {
	base    *url.URL
	path    string
	tokens  TokenSource
	http    *http.Client
	backoff Backoff
	bufSize int
	records chan LogRecord

	status  atomic.Int32
	started atomic.Bool

	mu      sync.Mutex
	lastErr error

	outage outage
}

// LogsConsumerOption customizes a LogsConsumer at construction.
type LogsConsumerOption func(*LogsConsumer)

// WithLogsHTTPClient supplies the underlying *http.Client (tests or a shared
// transport). A nil client is ignored. The stream is long-lived, so a client with
// a non-zero Timeout would sever a healthy stream — prefer the default.
func WithLogsHTTPClient(h *http.Client) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if h != nil {
			c.http = h
		}
	}
}

// WithLogsBackoff sets the reconnect backoff policy. A nil policy is ignored.
func WithLogsBackoff(b Backoff) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if b != nil {
			c.backoff = b
		}
	}
}

// WithLogsBuffer sets the records channel capacity. A non-positive value is
// ignored, keeping the default bound.
func WithLogsBuffer(n int) LogsConsumerOption {
	return func(c *LogsConsumer) {
		if n > 0 {
			c.bufSize = n
		}
	}
}

// NewLogsConsumer builds a log SSE consumer against baseURL, authenticating with
// tokens. It errors on an empty or unparseable baseURL or a nil TokenSource, so
// misconfiguration fails at startup rather than at first connect.
func NewLogsConsumer(baseURL string, tokens TokenSource, opts ...LogsConsumerOption) (*LogsConsumer, error) {
	if tokens == nil {
		return nil, errors.New("goapi: nil TokenSource")
	}
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	c := &LogsConsumer{
		base:    base,
		path:    operatorLogStreamPath,
		tokens:  tokens,
		http:    defaultSSEClient(),
		backoff: NewExpBackoff(defaultBackoffBase, defaultBackoffMax),
		bufSize: defaultEventBuffer,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.records = make(chan LogRecord, c.bufSize)
	return c, nil
}

// Records is the receive-only channel of decoded log records. Run closes it on
// exit, so a `range` over it terminates cleanly on shutdown.
func (c *LogsConsumer) Records() <-chan LogRecord { return c.records }

// Status returns the current connection state to the log stream.
func (c *LogsConsumer) Status() Status { return Status(c.status.Load()) }

// LastError returns the most recent connection failure (a *SourceDownError for an
// unreachable upstream, an *APIError for a non-2xx), or nil while healthy.
func (c *LogsConsumer) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Run streams log records until ctx is cancelled. It connects, emits decoded
// records on Records(), and on any disconnect waits a backoff interval and
// reconnects — resuming the stream. It returns ctx.Err() on
// shutdown and closes Records(). Run may be called at most once per consumer.
func (c *LogsConsumer) Run(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("goapi: logs consumer already running")
	}
	defer close(c.records)
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

// stream runs one connection attempt, returning whether a stream was actually
// established (a 200 body) so Run resets backoff only after real progress.
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

// pump reads records off body until the stream ends or ctx is cancelled, sending
// each on the records channel.
func (c *LogsConsumer) pump(ctx context.Context, body io.ReadCloser) {
	watchdog := watchIdle(ctx, body)
	defer watchdog.stop()
	dec := newLogSSEDecoder(watchdog)
	for {
		rec, err := dec.next()
		if err != nil {
			if ctx.Err() == nil {
				c.markDropped(&SourceDownError{Op: c.op(), Err: watchdog.cause(err)})
			}
			return
		}
		if !c.emit(ctx, rec) {
			return
		}
	}
}

// emit sends rec on the records channel, abandoning the send if ctx is cancelled
// so shutdown never blocks on a full buffer with no reader. A full buffer
// otherwise applies backpressure (bounded memory). Returns false when ctx is done.
func (c *LogsConsumer) emit(ctx context.Context, rec LogRecord) bool {
	select {
	case c.records <- rec:
		return true
	case <-ctx.Done():
		return false
	}
}

// connect issues the SSE GET with the read-only bearer token. A transport failure
// becomes a *SourceDownError; a non-2xx becomes an *APIError. The caller owns
// closing the body on success.
func (c *LogsConsumer) connect(ctx context.Context) (*http.Response, error) {
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
func (c *LogsConsumer) rejectStatus(resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	invalidateOn401(c.tokens, resp.StatusCode)
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return &APIError{Op: c.op(), StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
}

// wait blocks for the backoff interval before attempt n, returning early with
// ctx.Err() if the consumer is shut down mid-wait — so a pending backoff never
// delays a clean shutdown and its timer never leaks.
func (c *LogsConsumer) wait(ctx context.Context, attempt int) error {
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

func (c *LogsConsumer) op() string { return "GET " + c.path }

func (c *LogsConsumer) setStatus(s Status) { c.status.Store(int32(s)) }

func (c *LogsConsumer) markFailed(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.setStatus(c.outage.status())
}

func (c *LogsConsumer) markDropped(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	c.setStatus(c.outage.dropped())
}

func (c *LogsConsumer) markUp() {
	c.mu.Lock()
	c.lastErr = nil
	c.mu.Unlock()
	c.outage.connected()
	c.setStatus(StatusUp)
}
