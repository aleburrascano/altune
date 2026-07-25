package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const providerBodyCap = 2 << 20

type reqOption func(*http.Request)

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

func getBytes(ctx context.Context, client *http.Client, url string, opts ...reqOption) (int, []byte, error) {
	return getBytesCapped(ctx, client, url, providerBodyCap, opts...)
}

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
