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

// NtfyNotifier pushes alerts to an operator-configured, external ntfy topic.
// Everything it sends leaves the system, so callers must build Alert.Title and
// Alert.Message from aggregate, operator-safe facts only (counts, thresholds,
// dependency names) and never from user-supplied content such as search text.
type NtfyNotifier struct {
	url    string
	client *http.Client
}

// errInsecureNtfyURL is returned when the ntfy URL (or a redirect target) is
// not https: the topic in the path is a de-facto secret and must never travel
// in plaintext.
var errInsecureNtfyURL = errors.New("ntfy URL must use https")

// NewNtfyNotifier builds a notifier for rawURL, rejecting anything that is not
// an absolute https URL. Redirects to a non-https target are refused too, so a
// misbehaving server cannot downgrade the push to plaintext.
func NewNtfyNotifier(rawURL string) (*NtfyNotifier, error) {
	if err := requireHTTPS(rawURL); err != nil {
		return nil, err
	}
	return &NtfyNotifier{
		url: rawURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return errInsecureNtfyURL
				}
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				return nil
			},
		},
	}, nil
}

func requireHTTPS(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid ntfy URL %q", maskURL(rawURL))
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w, got scheme %q", errInsecureNtfyURL, u.Scheme)
	}
	return nil
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
