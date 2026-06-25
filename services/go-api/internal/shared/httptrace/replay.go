package httptrace

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Replayer is an http.RoundTripper that answers each request from a set of
// previously-recorded Exchanges instead of hitting the network. It is the replay
// half of Recorder: record once against live providers, then replay the captured
// exchanges through the real pipeline for a deterministic, offline run.
//
// Requests are matched on (method, URL, request body) — the same fields a
// provider reconstructs deterministically from a fixed query and fixed API
// credentials. Repeated identical requests are replayed in capture order (FIFO),
// so pagination and retries reproduce faithfully.
//
// A request with no matching recorded exchange is a hard error, never a silent
// empty response: a missing fixture must surface loudly so a deterministic eval
// can trust that what it replayed is what was recorded.
type Replayer struct {
	mu     sync.Mutex
	queues map[string][]Exchange
}

// NewReplayer indexes the recorded exchanges by match key, preserving capture
// order within each key.
func NewReplayer(exchanges []Exchange) *Replayer {
	queues := make(map[string][]Exchange, len(exchanges))
	for _, ex := range exchanges {
		k := matchKey(ex.Method, ex.URL, ex.ReqBody)
		queues[k] = append(queues[k], ex)
	}
	return &Replayer{queues: queues}
}

// RoundTrip returns the next recorded response for the request's match key. An
// unmatched request (no key, or the key's queue is exhausted) returns an error.
func (r *Replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBody := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		reqBody = string(b)
	}
	k := matchKey(req.Method, req.URL.String(), reqBody)

	r.mu.Lock()
	q := r.queues[k]
	if len(q) == 0 {
		r.mu.Unlock()
		return nil, fmt.Errorf("httptrace: no recorded exchange for %s %s", req.Method, req.URL)
	}
	ex := q[0]
	r.queues[k] = q[1:]
	r.mu.Unlock()

	if ex.Err != "" {
		return nil, fmt.Errorf("httptrace: recorded transport error: %s", ex.Err)
	}

	resp := &http.Response{
		StatusCode:    ex.Status,
		Status:        http.StatusText(ex.Status),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader(ex.RespBody)),
		ContentLength: int64(len(ex.RespBody)),
		Request:       req,
	}
	return resp, nil
}

// Remaining reports how many recorded exchanges were never replayed — a non-zero
// count after a run means the replay diverged from what was recorded (a provider
// asked for fewer/other URLs than at capture time).
func (r *Replayer) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.queues {
		n += len(q)
	}
	return n
}

// matchKey is the request identity used for record/replay correlation: method,
// URL, and request body (for POST/GraphQL providers whose query lives in the
// body, not the URL).
func matchKey(method, url, body string) string {
	return method + "\n" + url + "\n" + body
}
