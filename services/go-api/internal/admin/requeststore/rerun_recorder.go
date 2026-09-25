package requeststore

import (
	"altune/go-api/internal/shared/redact"
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

type RerunRecorder struct {
	base    http.RoundTripper
	bodyCap int

	mu        sync.Mutex
	exchanges []Exchange
}

func NewRerunRecorder(base http.RoundTripper, bodyCap int) *RerunRecorder {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RerunRecorder{base: base, bodyCap: bodyCap, exchanges: []Exchange{}}
}

func (r *RerunRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := r.base.RoundTrip(req)
	ex := Exchange{
		Method:    req.Method,
		URL:       redact.Secrets(req.URL.String()),
		LatencyMs: time.Since(start).Milliseconds(),
		At:        start.UTC(),
	}
	if err != nil || resp == nil {
		if err != nil {
			ex.Err = redact.Secrets(err.Error())
		}
		r.add(ex)
		return resp, err
	}

	r.recordBody(&ex, resp)
	r.add(ex)
	return resp, nil
}

func (r *RerunRecorder) recordBody(ex *Exchange, resp *http.Response) {
	ex.Status = resp.StatusCode
	var readErr error
	ex.RespBody, ex.Truncated, readErr = r.capture(resp)
	if readErr != nil {
		ex.Err = redact.Secrets(readErr.Error())
	}
}

// capture reads at most bodyCap+1 bytes of resp.Body (the extra byte detects
// truncation without buffering the rest), then re-stitches that prefix in
// front of the unread remainder so the caller still receives the full stream.
func (r *RerunRecorder) capture(resp *http.Response) (string, bool, error) {
	limit := max(r.bodyCap, 0)
	prefix, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	resp.Body = prefixedBody{Reader: io.MultiReader(bytes.NewReader(prefix), resp.Body), Closer: resp.Body}
	if len(prefix) > limit {
		return RedactBody(string(prefix[:limit])), true, readErr
	}
	return RedactBody(string(prefix)), false, readErr
}

type prefixedBody struct {
	io.Reader
	io.Closer
}

func (r *RerunRecorder) add(ex Exchange) {
	r.mu.Lock()
	r.exchanges = append(r.exchanges, ex)
	r.mu.Unlock()
}

func (r *RerunRecorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Exchange, len(r.exchanges))
	copy(out, r.exchanges)
	return out
}
