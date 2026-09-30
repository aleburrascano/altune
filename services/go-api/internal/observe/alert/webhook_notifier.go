package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const webhookTimeout = 5 * time.Second

type WebhookNotifier struct {
	url    string
	client *http.Client
}

func NewWebhookNotifier(webhookURL string) *WebhookNotifier {
	return &WebhookNotifier{url: webhookURL, client: &http.Client{Timeout: webhookTimeout}}
}

type webhookPayload struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Severity int    `json:"severity"`
}

func (n *WebhookNotifier) Notify(ctx context.Context, a Alert) error {
	body, err := json.Marshal(webhookPayload{Title: a.Title, Message: a.Message, Severity: int(a.Severity)})
	if err != nil {
		return fmt.Errorf("encode alert: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build alert request: %w", withoutURL(err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post alert: %w", withoutURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("post alert: status %d", resp.StatusCode)
	}
	return nil
}

func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
