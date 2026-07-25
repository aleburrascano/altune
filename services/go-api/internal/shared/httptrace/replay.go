package httptrace

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type Replayer struct {
	mu     sync.Mutex
	queues map[string][]Exchange
}

func NewReplayer(exchanges []Exchange) *Replayer {
	queues := make(map[string][]Exchange, len(exchanges))
	for _, ex := range exchanges {
		k := matchKey(ex.Method, ex.URL, ex.ReqBody)
		queues[k] = append(queues[k], ex)
	}
	return &Replayer{queues: queues}
}

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

func (r *Replayer) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.queues {
		n += len(q)
	}
	return n
}

func matchKey(method, url, body string) string {
	return method + "\n" + url + "\n" + body
}
