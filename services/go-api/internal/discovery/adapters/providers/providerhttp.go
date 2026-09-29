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

type httpStatusError struct {
	status int
}

func (e httpStatusError) Error() string { return fmt.Sprintf("http status %d", e.status) }

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

func sendRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, errWithoutURLQuery(err)
	}
	return resp, nil
}

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
