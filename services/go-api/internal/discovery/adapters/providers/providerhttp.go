package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const providerBodyCap = 2 << 20

// httpStatusError is the non-200 response error the shared request helpers
// return. It carries the status so callers outside the adapter (the discovery
// circuit breaker) can tell an unhealthy upstream (5xx, 429) apart from a
// request-scoped rejection such as a 404 for an unknown ID, without parsing
// the message.
type httpStatusError struct {
	status int
}

func (e httpStatusError) Error() string { return fmt.Sprintf("http status %d", e.status) }

// HTTPStatus reports the upstream HTTP status code.
func (e httpStatusError) HTTPStatus() int { return e.status }

func isAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

type reqOption func(*http.Request)

func withHeader(key, value string) reqOption {
	return func(r *http.Request) {
		if value != "" {
			r.Header.Set(key, value)
		}
	}
}

func newGetRequest(ctx context.Context, rawURL string, opts ...reqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

func newPostRequest(ctx context.Context, rawURL string, body io.Reader, opts ...reqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

// sendRequest is the one place these helpers reach the network, so a transport
// failure cannot reach a call site still carrying the credential Last.fm,
// fanart.tv and SoundCloud pass in the query string.
func sendRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, errWithoutURLQuery(err)
	}
	return resp, nil
}

// errWithoutURLQuery drops the query from the request URL that a *url.Error
// echoes: the whole query, not the params a redaction vocabulary knows, so a
// provider naming its key something new still cannot leak it. Host and path
// survive for diagnosis. It edits in place because client.Do allocates that
// error per call, which keeps the unwrap chain (context.DeadlineExceeded,
// net.Error) intact for callers that branch on it.
func errWithoutURLQuery(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		urlErr.URL = withoutQuery(urlErr.URL)
	}
	return err
}

func withoutQuery(rawURL string) string {
	if queryStart := strings.IndexByte(rawURL, '?'); queryStart >= 0 {
		return rawURL[:queryStart]
	}
	return rawURL
}

func getJSON(ctx context.Context, client *http.Client, rawURL string, dst any, opts ...reqOption) error {
	_, err := getJSONWithStatus(ctx, client, rawURL, dst, opts...)
	return err
}

// getJSONWithStatus is getJSON for callers that branch on the HTTP status
// (e.g. auth retry). Status is 0 when no response arrived; a non-nil error
// with status 200 is a decode failure.
func getJSONWithStatus(ctx context.Context, client *http.Client, rawURL string, dst any, opts ...reqOption) (int, error) {
	req, err := newGetRequest(ctx, rawURL, opts...)
	if err != nil {
		return 0, err
	}
	resp, err := sendRequest(client, req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, httpStatusError{status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, providerBodyCap)).Decode(dst); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func getBytes(ctx context.Context, client *http.Client, rawURL string, opts ...reqOption) (int, []byte, error) {
	return getBytesCapped(ctx, client, rawURL, providerBodyCap, opts...)
}

func getBytesCapped(ctx context.Context, client *http.Client, rawURL string, limit int64, opts ...reqOption) (int, []byte, error) {
	req, err := newGetRequest(ctx, rawURL, opts...)
	if err != nil {
		return 0, nil, err
	}
	resp, err := sendRequest(client, req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body, httpStatusError{status: resp.StatusCode}
	}
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, body, nil
}

func postJSON(ctx context.Context, client *http.Client, rawURL string, body []byte, dst any, opts ...reqOption) (int, error) {
	req, err := newPostRequest(ctx, rawURL, bytes.NewReader(body), opts...)
	if err != nil {
		return 0, err
	}
	resp, err := sendRequest(client, req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, httpStatusError{status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, providerBodyCap)).Decode(dst); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

// postBytesCappedOK is postBytesCapped for callers that treat any non-200 as
// an "http status N" error, the POST twin of getBytesCapped's status gate.
// Transport and read errors still take precedence over the status check;
// the status is returned alongside the error so callers can branch on auth.
func postBytesCappedOK(ctx context.Context, client *http.Client, rawURL string, body io.Reader, limit int64, opts ...reqOption) (int, []byte, error) {
	status, data, err := postBytesCapped(ctx, client, rawURL, body, limit, opts...)
	if err != nil {
		return status, data, err
	}
	if status != http.StatusOK {
		return status, data, httpStatusError{status: status}
	}
	return status, data, nil
}

// postBytesCapped does not gate on status: callers such as the Deezer lyrics
// auth retry and the YouTube Music decoder branch on non-200 responses.
func postBytesCapped(ctx context.Context, client *http.Client, rawURL string, body io.Reader, limit int64, opts ...reqOption) (int, []byte, error) {
	req, err := newPostRequest(ctx, rawURL, body, opts...)
	if err != nil {
		return 0, nil, err
	}
	resp, err := sendRequest(client, req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, data, nil
}
