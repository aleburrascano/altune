package savedlink

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxPageBytes = 1 << 20

var (
	errRedirect   = errors.New("file host page redirected")
	errNoResponse = errors.New("file host page gave no response")
)

type HTTPPageReader struct {
	client *http.Client
}

func NewHTTPPageReader(transport http.RoundTripper, timeout time.Duration) *HTTPPageReader {
	client := outbound.MustNewClient(timeout, transport)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	return &HTTPPageReader{client: client}
}

func (r *HTTPPageReader) ReadPage(ctx context.Context, pageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build file host page request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch file host page: %w", err)
	}
	if resp == nil {
		return nil, errNoResponse
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file host page answered %d", resp.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("read file host page: %w", err)
	}
	return page, nil
}
