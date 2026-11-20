package notifier

import (
	"altune/go-api/internal/observe/internal/core/domain"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	resendEmailsURL    = "https://api.resend.com/emails"
	alertEmailFrom     = "altune alerts <onboarding@resend.dev>"
	emailTimeout       = 10 * time.Second
	maxEmailReplyBytes = 4 << 10
)

type EmailNotifier struct {
	apiKey string
	to     string
	client *http.Client
}

func NewEmailNotifier(apiKey, to string, transport http.RoundTripper) *EmailNotifier {
	return &EmailNotifier{apiKey: apiKey, to: to, client: &http.Client{Timeout: emailTimeout, Transport: transport}}
}

type resendEmail struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

type resendError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (n *EmailNotifier) Notify(ctx context.Context, a domain.Alert) error {
	req, err := n.buildRequest(ctx, a)
	if err != nil {
		return err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send alert email: %w", err)
	}
	if resp == nil {
		return errors.New("send alert email: no response")
	}
	defer func() { _ = resp.Body.Close() }()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, maxEmailReplyBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return n.rejection(resp.StatusCode, reply)
	}
	return nil
}

func (n *EmailNotifier) buildRequest(ctx context.Context, a domain.Alert) (*http.Request, error) {
	body, err := json.Marshal(resendEmail{
		From:    alertEmailFrom,
		To:      n.to,
		Subject: a.Title,
		Text:    a.Message + "\n\nSeverity: " + a.Severity.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("encode alert email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEmailsURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build alert email request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (n *EmailNotifier) rejection(status int, reply []byte) error {
	var detail resendError
	if json.Unmarshal(reply, &detail) != nil || detail.Message == "" {
		return fmt.Errorf("send alert email: status %d", status)
	}
	return fmt.Errorf("send alert email: status %d: %s", status, n.withoutKey(detail.Name+": "+detail.Message))
}

func (n *EmailNotifier) withoutKey(text string) string {
	if n.apiKey == "" {
		return text
	}
	return strings.ReplaceAll(text, n.apiKey, "[redacted]")
}
