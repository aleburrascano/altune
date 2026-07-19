package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// providerhttp is the single home for the GET → status-check → decode/read
// transport dance that every provider adapter repeated. Per-provider quirks
// (auth headers, user-agent) are options; rate-limiting stays at the call site
// because it is adapter policy, not transport.
//
// Rate-limiting is deliberately per-provider, not uniform: MusicBrainz reserves
// 1 req/sec (its terms require it), Discogs detects and surfaces 429s, and the
// rest rely on the shared client timeout plus the search circuit breaker. The
// absence of an explicit limiter on a provider is intentional, not an oversight
// — each reflects that provider's published contract.
//
// Two shapes:
//
//   - getJSON  streams-decodes a 200 JSON body (no body cap — matches the prior
//     search/lookup behaviour).
//   - getBytes reads a size-capped body and returns the status, so callers can
//     branch on it (e.g. 429) — matches the prior bytes-returning helpers.

const providerBodyCap = 2 << 20 // 2 MiB, for the bytes path

type reqOption func(*http.Request)

// withHeader sets a request header when the value is non-empty.
func withHeader(key, value string) reqOption {
	return func(r *http.Request) {
		if value != "" {
			r.Header.Set(key, value)
		}
	}
}

func newGetRequest(ctx context.Context, url string, opts ...reqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

// getJSON performs a GET and decodes a 200 JSON body into dst. A non-200 status
// or a transport error is returned as an error.
func getJSON(ctx context.Context, client *http.Client, url string, dst any, opts ...reqOption) error {
	req, err := newGetRequest(ctx, url, opts...)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// getBytes performs a GET and returns the status plus the size-capped body. The
// body is returned even on a non-200 status (with a non-nil error) so callers
// can inspect the status; on a transport error the status is 0.
func getBytes(ctx context.Context, client *http.Client, url string, opts ...reqOption) (int, []byte, error) {
	return getBytesCapped(ctx, client, url, providerBodyCap, opts...)
}

// getBytesCapped is getBytes with a caller-chosen body cap, for the rare
// payload that exceeds the default 2 MiB (e.g. SoundCloud's JS asset bundles).
func getBytesCapped(ctx context.Context, client *http.Client, url string, cap int64, opts ...reqOption) (int, []byte, error) {
	req, err := newGetRequest(ctx, url, opts...)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, cap))
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body, fmt.Errorf("http status %d", resp.StatusCode)
	}
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, body, nil
}
