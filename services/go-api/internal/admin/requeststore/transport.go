package requeststore

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

type correlatedTransport struct {
	base  http.RoundTripper
	store *Store
}

func NewCorrelatedTransport(base http.RoundTripper, store *Store) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &correlatedTransport{base: base, store: store}
}

func (t *correlatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	corrID := logging.CorrelationIDFromContext(req.Context())
	if corrID == "" || t.store == nil {
		return t.base.RoundTrip(req)
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	latency := time.Since(start).Milliseconds()
	ex := Exchange{Method: req.Method, URL: redact.Secrets(req.URL.String()), LatencyMs: latency, At: start.UTC()}

	if err != nil {
		ex.Err = redact.Secrets(err.Error())
		t.store.recordExchange(corrID, ex, start)
		return resp, err
	}

	ex.Status = resp.StatusCode
	resp.Body = &capturingBody{
		inner:  resp.Body,
		buf:    &bytes.Buffer{},
		cap:    t.store.MaxBodyBytes(),
		store:  t.store,
		corrID: corrID,
		ex:     ex,
		start:  start,
	}
	return resp, nil
}

// capturingBody tees a response body into a capped buffer. An http.Response
// body can legally be read on one goroutine and closed on another, so mu
// guards every field the two paths share.
type capturingBody struct {
	inner  io.ReadCloser
	cap    int
	store  *Store
	corrID string
	start  time.Time // monotonic-bearing; ex.At is its wall-only UTC form

	mu    sync.Mutex
	ex    Exchange
	buf   *bytes.Buffer
	trunc bool
	done  bool

	readErr string
}

func (c *capturingBody) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		c.capture(p[:n])
	}
	if err != nil && err != io.EOF {
		c.noteReadError(err)
	}
	return n, err
}

func (c *capturingBody) noteReadError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readErr = redact.Secrets(err.Error())
}

func (c *capturingBody) capture(read []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := max(c.cap-c.buf.Len(), 0)
	if len(read) > room {
		c.trunc = true
		read = read[:room]
	}
	c.buf.Write(read)
}

func (c *capturingBody) Close() error {
	if ex, first := c.sealCapture(); first {
		c.store.recordExchange(c.corrID, ex, c.start)
	}
	return c.inner.Close()
}

// sealCapture returns the exchange to record and false on every Close after the
// first, so a double Close — even a concurrent one — records the exchange once.
func (c *capturingBody) sealCapture() (Exchange, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return Exchange{}, false
	}
	c.done = true
	c.ex.RespBody = RedactBody(c.buf.String())
	c.ex.Truncated = c.trunc
	if c.ex.Err == "" {
		c.ex.Err = c.readErr
	}
	return c.ex, true
}
