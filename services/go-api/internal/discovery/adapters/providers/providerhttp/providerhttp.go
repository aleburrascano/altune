package providerhttp

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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

const BodyCap = 2 << 20

type StatusError struct {
	Status int
}

func (e StatusError) Error() string { return fmt.Sprintf("http status %d", e.Status) }

func (e StatusError) HTTPStatus() int { return e.Status }

type PersistentAuthError struct{ Cause error }

func (e PersistentAuthError) Error() string { return e.Cause.Error() }

func (e PersistentAuthError) Unwrap() error { return e.Cause }

func (PersistentAuthError) PersistentAuthFailure() bool { return true }

type BadPayloadError struct{ Cause error }

func (e BadPayloadError) Error() string { return e.Cause.Error() }

func (e BadPayloadError) Unwrap() error { return e.Cause }

func (BadPayloadError) BadPayload() bool { return true }

func IsAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

type ReqOption func(*http.Request)

func WithHeader(key, value string) ReqOption {
	return func(r *http.Request) {
		if value != "" {
			r.Header.Set(key, value)
		}
	}
}

func NewGetRequest(ctx context.Context, rawURL string, opts ...ReqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

func NewHeadRequest(ctx context.Context, rawURL string, opts ...ReqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

func NewPostRequest(ctx context.Context, rawURL string, body io.Reader, opts ...ReqOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return req, nil
}

func Send(client *http.Client, req *http.Request) (*http.Response, error) {
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

func GetJSON(ctx context.Context, client *http.Client, rawURL string, dst any, opts ...ReqOption) error {
	_, err := GetJSONWithStatus(ctx, client, rawURL, dst, opts...)
	return err
}

func GetJSONWithStatus(ctx context.Context, client *http.Client, rawURL string, dst any, opts ...ReqOption) (int, error) {
	req, err := NewGetRequest(ctx, rawURL, opts...)
	if err != nil {
		return 0, err
	}
	resp, err := Send(client, req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, StatusError{Status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, BodyCap)).Decode(dst); err != nil {
		return resp.StatusCode, BadPayloadError{Cause: err}
	}
	return resp.StatusCode, nil
}

func GetBytes(ctx context.Context, client *http.Client, rawURL string, opts ...ReqOption) (int, []byte, error) {
	return GetBytesCapped(ctx, client, rawURL, BodyCap, opts...)
}

func GetBytesCapped(ctx context.Context, client *http.Client, rawURL string, limit int64, opts ...ReqOption) (int, []byte, error) {
	req, err := NewGetRequest(ctx, rawURL, opts...)
	if err != nil {
		return 0, nil, err
	}
	resp, err := Send(client, req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body, StatusError{Status: resp.StatusCode}
	}
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, body, nil
}

func GetText(ctx context.Context, client *http.Client, rawURL string, limit int64, userAgent string) (string, error) {
	status, body, err := GetBytesCapped(ctx, client, rawURL, limit, WithHeader("User-Agent", userAgent))
	if err != nil {
		if status != 0 {
			return "", fmt.Errorf("GET %s: status %d", rawURL, status)
		}
		return "", err
	}
	return string(body), nil
}

func PostJSON(ctx context.Context, client *http.Client, rawURL string, body []byte, dst any, opts ...ReqOption) (int, error) {
	req, err := NewPostRequest(ctx, rawURL, bytes.NewReader(body), opts...)
	if err != nil {
		return 0, err
	}
	resp, err := Send(client, req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, StatusError{Status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, BodyCap)).Decode(dst); err != nil {
		return resp.StatusCode, BadPayloadError{Cause: err}
	}
	return resp.StatusCode, nil
}

func PostBytesCappedOK(ctx context.Context, client *http.Client, rawURL string, body io.Reader, limit int64, opts ...ReqOption) (int, []byte, error) {
	status, payload, err := PostBytesCapped(ctx, client, rawURL, body, limit, opts...)
	if err != nil {
		return status, payload, err
	}
	if status != http.StatusOK {
		return status, payload, StatusError{Status: status}
	}
	return status, payload, nil
}

func PostBytesCapped(ctx context.Context, client *http.Client, rawURL string, body io.Reader, limit int64, opts ...ReqOption) (int, []byte, error) {
	req, err := NewPostRequest(ctx, rawURL, body, opts...)
	if err != nil {
		return 0, nil, err
	}
	resp, err := Send(client, req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	if readErr != nil {
		return resp.StatusCode, nil, readErr
	}
	return resp.StatusCode, payload, nil
}

func ArtworkFailure(source domain.ProviderKey, err error) (string, error) {
	var statusErr StatusError
	if errors.As(err, &statusErr) {
		if statusErr.HTTPStatus() == http.StatusNotFound || statusErr.HTTPStatus() == http.StatusBadRequest {
			return "", nil
		}
	}
	return "", fmt.Errorf("%w: %s: %w", ports.ErrArtworkUnavailable, source, err)
}
