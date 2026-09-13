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
	if err != nil {
		ex.Err = redact.Secrets(err.Error())
		r.add(ex)
		return resp, err
	}

	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	ex.Status = resp.StatusCode
	captured := body
	if len(body) > r.bodyCap {
		captured = body[:r.bodyCap]
		ex.Truncated = true
	}
	ex.RespBody = RedactBody(string(captured))
	r.add(ex)
	return resp, nil
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
