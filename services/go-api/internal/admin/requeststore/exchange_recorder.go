package requeststore

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

// ExchangeRecorder is a one-shot recording RoundTripper for the re-run inspector:
// it captures every exchange (body capped) in call order into a flat list. Unlike
// the corr_id-keyed transport it is scoped to a single operator-triggered re-run,
// so a full-body read is acceptable — the bodies are capped and the recorder is
// discarded after the response is serialized.
type ExchangeRecorder struct {
	base    http.RoundTripper
	bodyCap int

	mu        sync.Mutex
	exchanges []Exchange
}

// NewExchangeRecorder wraps base, capping each retained body at bodyCap bytes.
func NewExchangeRecorder(base http.RoundTripper, bodyCap int) *ExchangeRecorder {
	if base == nil {
		base = http.DefaultTransport
	}
	return &ExchangeRecorder{base: base, bodyCap: bodyCap, exchanges: []Exchange{}}
}

func (r *ExchangeRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := r.base.RoundTrip(req)
	ex := Exchange{
		Method:    req.Method,
		URL:       req.URL.String(),
		LatencyMs: time.Since(start).Milliseconds(),
		At:        start.UTC(),
	}
	if err != nil {
		ex.Err = err.Error()
		r.add(ex)
		return resp, err
	}

	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body)) // restore full body to the caller
	ex.Status = resp.StatusCode
	if len(body) > r.bodyCap {
		ex.RespBody = string(body[:r.bodyCap])
		ex.Truncated = true
	} else {
		ex.RespBody = string(body)
	}
	r.add(ex)
	return resp, nil
}

func (r *ExchangeRecorder) add(ex Exchange) {
	r.mu.Lock()
	r.exchanges = append(r.exchanges, ex)
	r.mu.Unlock()
}

// Exchanges returns a copy of everything recorded, in call order.
func (r *ExchangeRecorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Exchange, len(r.exchanges))
	copy(out, r.exchanges)
	return out
}
