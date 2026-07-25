package httptrace

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

type Exchange struct {
	Method   string        `json:"method"`
	URL      string        `json:"url"`
	Status   int           `json:"status"`
	Duration time.Duration `json:"duration_ns"`
	ReqBody  string        `json:"request_body,omitempty"`
	RespBody string        `json:"response_body"`
	Err      string        `json:"error,omitempty"`
}

type Recorder struct {
	base http.RoundTripper

	mu        sync.Mutex
	exchanges []Exchange
}

func NewRecorder(base http.RoundTripper) *Recorder {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Recorder{base: base}
}

func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	ex := Exchange{Method: req.Method, URL: req.URL.String()}

	if req.Body != nil {
		reqBytes, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBytes))
		ex.ReqBody = string(reqBytes)
	}

	start := time.Now()
	resp, err := r.base.RoundTrip(req)
	ex.Duration = time.Since(start)

	if err != nil {
		ex.Err = err.Error()
		r.record(ex)
		return resp, err
	}

	respBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBytes))
	ex.Status = resp.StatusCode
	ex.RespBody = string(respBytes)
	if readErr != nil {
		ex.Err = readErr.Error()
	}
	r.record(ex)
	return resp, nil
}

func (r *Recorder) record(ex Exchange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exchanges = append(r.exchanges, ex)
}

func (r *Recorder) Exchanges() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Exchange, len(r.exchanges))
	copy(out, r.exchanges)
	return out
}
