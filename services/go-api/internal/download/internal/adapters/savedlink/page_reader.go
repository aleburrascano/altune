package savedlink

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const maxPageBytes = 1 << 20

var errRedirect = errors.New("file host page redirected")

type HTTPPageReader struct {
	client *http.Client
}

func NewHTTPPageReader(transport http.RoundTripper, timeout time.Duration) *HTTPPageReader {
	client := outbound.MustNewClient(timeout, transport)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	return &HTTPPageReader{client: client}
}

func (r *HTTPPageReader) ReadPage(ctx context.Context, pageURL string) ([]byte, error) {
	status, page, err := outbound.GetBytesCapped(ctx, r.client, pageURL, maxPageBytes)
	var statusErr outbound.StatusError
	switch {
	case errors.As(err, &statusErr):
		return nil, fmt.Errorf("file host page answered %d", status)
	case err != nil:
		return nil, fmt.Errorf("fetch file host page: %w", err)
	}
	return page, nil
}
