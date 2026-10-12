package httptrace

import (
	"altune/go-api/internal/shared/redact"
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

const maxCaptureBytes = 1 << 20

type replayBody struct {
	io.Reader
	io.Closer
}

const truncatedBody = "[TRUNCATED: body exceeds capture limit]"

const truncatedErr = "body exceeds capture limit; recording is incomplete"

func capture(body io.ReadCloser) (string, io.ReadCloser, bool, error) {
	read, err := io.ReadAll(io.LimitReader(body, maxCaptureBytes+1))
	replay := replayBody{io.MultiReader(bytes.NewReader(read), body), body}
	if len(read) > maxCaptureBytes {
		return truncatedBody, replay, true, err
	}
	return redact.SecretsInBody(string(read)), replay, false, err
}

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
	ex := Exchange{Method: req.Method, URL: redact.Secrets(req.URL.String())}

	if req.Body != nil {
		reqBody, replay, reqTruncated, _ := capture(req.Body)
		req.Body = replay
		ex.ReqBody = reqBody
		if reqTruncated {
			ex.Err = truncatedErr
		}
	}

	start := time.Now()
	resp, err := r.base.RoundTrip(req)
	ex.Duration = time.Since(start)

	if err != nil {
		ex.Err = joinErr(ex.Err, redact.Secrets(err.Error()))
		r.record(ex)
		return resp, err
	}

	respBody, replay, respTruncated, readErr := capture(resp.Body)
	resp.Body = replay
	ex.Status = resp.StatusCode
	ex.RespBody = respBody
	if respTruncated {
		ex.Err = joinErr(ex.Err, truncatedErr)
	}
	if readErr != nil {
		ex.Err = joinErr(ex.Err, redact.Secrets(readErr.Error()))
	}
	r.record(ex)
	return resp, nil
}

func joinErr(prev, next string) string {
	if prev == "" {
		return next
	}
	return prev + "; " + next
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
