package alert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, Alert) error { return nil }

type NtfyNotifier struct {
	url    string
	client *http.Client
}

func NewNtfyNotifier(url string) *NtfyNotifier {
	return &NtfyNotifier{
		url:    url,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (n *NtfyNotifier) Notify(ctx context.Context, a Alert) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader([]byte(a.Message)))
	if err != nil {
		return fmt.Errorf("build ntfy request: %w", err)
	}
	req.Header.Set("Title", a.Title)
	req.Header.Set("Priority", "urgent")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post ntfy: %w", maskURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}
	return nil
}

// maskURLError strips the full request URL out of a *url.Error so the ntfy
// topic (a de-facto secret) never reaches operator-readable logs. Only the
// scheme and host survive; the path and query are replaced with a marker.
func maskURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	masked := *urlErr
	masked.URL = maskURL(urlErr.URL)
	return &masked
}

func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted]"
	}
	return u.Scheme + "://" + u.Host + "/[redacted]"
}
