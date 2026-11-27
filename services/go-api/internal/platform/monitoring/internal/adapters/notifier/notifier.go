package notifier

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/outbound"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type NopNotifier struct{}

func (NopNotifier) Silent() bool { return true }

func (NopNotifier) Notify(ctx context.Context, a domain.Alert) error {
	slog.ErrorContext(ctx, "alert.signal", "title", a.Title, "message", a.Message)
	return nil
}

const (
	webhookTimeout  = 5 * time.Second
	webhookReplyCap = 1 << 16
)

type WebhookNotifier struct {
	url    string
	client *http.Client
}

func NewWebhookNotifier(webhookURL string) *WebhookNotifier {
	return &WebhookNotifier{url: webhookURL, client: outbound.MustNewClient(webhookTimeout, nil)}
}

type webhookPayload struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Severity int    `json:"severity"`
}

func (n *WebhookNotifier) buildRequest(ctx context.Context, a domain.Alert) (*http.Request, error) {
	body, err := json.Marshal(webhookPayload{Title: a.Title, Message: a.Message, Severity: int(a.Severity)})
	if err != nil {
		return nil, fmt.Errorf("encode alert: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build alert request: %w", withoutURL(err))
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (n *WebhookNotifier) Notify(ctx context.Context, a domain.Alert) error {
	req, err := n.buildRequest(ctx, a)
	if err != nil {
		return err
	}
	status, _, err := outbound.DoCapped(n.client, req, webhookReplyCap)
	if err != nil && status == 0 {
		return fmt.Errorf("post alert: %w", withoutURL(err))
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("post alert: status %d", status)
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
