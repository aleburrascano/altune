package requeststore

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

type ExchangeRecorder struct {
	base    http.RoundTripper
	bodyCap int

	mu        sync.Mutex
	exchanges []Exchange
}

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
	resp.Body = io.NopCloser(bytes.NewReader(body))
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

func (r *ExchangeRecorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Exchange, len(r.exchanges))
	copy(out, r.exchanges)
	return out
}
