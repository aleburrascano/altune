package requeststore

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"altune/go-api/internal/shared/httputil"
)

type recordingTransport struct {
	base  http.RoundTripper
	store *Store
}

func NewTransport(base http.RoundTripper, store *Store) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &recordingTransport{base: base, store: store}
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	corrID := httputil.GetCorrelationID(req.Context())
	if corrID == "" || t.store == nil {
		return t.base.RoundTrip(req)
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	latency := time.Since(start).Milliseconds()
	ex := Exchange{Method: req.Method, URL: req.URL.String(), LatencyMs: latency, At: start.UTC()}

	if err != nil {
		ex.Err = err.Error()
		t.store.recordExchange(corrID, ex)
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
	}
	return resp, nil
}

type capturingBody struct {
	inner  io.ReadCloser
	buf    *bytes.Buffer
	cap    int
	trunc  bool
	store  *Store
	corrID string
	ex     Exchange
	done   bool
}

func (c *capturingBody) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		room := c.cap - c.buf.Len()
		if room <= 0 {
			c.trunc = true
		} else if n > room {
			c.buf.Write(p[:room])
			c.trunc = true
		} else {
			c.buf.Write(p[:n])
		}
	}
	return n, err
}

func (c *capturingBody) Close() error {
	if !c.done {
		c.done = true
		c.ex.RespBody = c.buf.String()
		c.ex.Truncated = c.trunc
		c.store.recordExchange(c.corrID, c.ex)
	}
	return c.inner.Close()
}
