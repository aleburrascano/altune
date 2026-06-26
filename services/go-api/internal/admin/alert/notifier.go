package alert

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// NopNotifier is used when no push channel is configured. The monitor still
// runs and logs firing conditions; nothing is pushed out of band.
type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, Alert) error { return nil }

// NtfyNotifier pushes alerts to an ntfy topic URL via a plain HTTP POST. The
// topic should be a non-guessable random string supplied via configuration.
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
		return fmt.Errorf("post ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}
	return nil
}
