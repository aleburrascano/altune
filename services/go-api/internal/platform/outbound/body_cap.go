package outbound

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrBodyTooLarge = errors.New("outbound response body exceeds the client body cap")

var errBodyCapNotPositive = errors.New("outbound client body cap must be positive")

type ClientOption func(*clientConfig) error

type clientConfig struct {
	bodyCap int64
}

func WithBodyCap(limit int64) ClientOption {
	return func(c *clientConfig) error {
		if limit <= 0 {
			return errBodyCapNotPositive
		}
		c.bodyCap = limit
		return nil
	}
}

type bodyCapTransport struct {
	next  http.RoundTripper
	limit int64
}

func newBodyCapTransport(next http.RoundTripper, limit int64) http.RoundTripper {
	if next == nil {
		next = DefaultTransport()
	}
	return bodyCapTransport{next: next, limit: limit}
}

func (t bodyCapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if resp.ContentLength > t.limit {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %d bytes announced, cap %d", ErrBodyTooLarge, resp.ContentLength, t.limit)
	}
	resp.Body = &cappedBody{body: resp.Body, left: t.limit}
	return resp, nil
}

type cappedBody struct {
	body io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	window := p
	if int64(len(window)) > b.left+1 {
		window = window[:b.left+1]
	}
	n, err := b.body.Read(window)
	if int64(n) > b.left {
		allowed := int(b.left)
		b.left = 0
		return allowed, ErrBodyTooLarge
	}
	b.left -= int64(n)
	return n, err
}

func (b *cappedBody) Close() error { return b.body.Close() }
