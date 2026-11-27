package alerts_test

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureText(t, slog.LevelInfo)
}

func isLogOnly(n monitoring.AlertNotifier) bool {
	_, nop := n.(monitoring.NopNotifier)
	return nop
}

func TestNewNotifier_AWebhookURLSendsAlertsToThatWebhook(t *testing.T) {
	var posts atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { posts.Add(1) }))
	defer hook.Close()

	n := alerts.NewNotifier(context.Background(), &config.Config{AlertWebhookURL: hook.URL})

	if err := n.Notify(context.Background(), monitoring.Alert{Title: "t"}); err != nil {
		t.Fatalf("Notify = %v, want nil", err)
	}
	if posts.Load() != 1 {
		t.Fatalf("webhook got %d posts, want 1", posts.Load())
	}
}

func TestNewNotifier_EmailSettingsBuildARealNotifierWithoutTheLogOnlyWarning(t *testing.T) {
	logged := captureLogs(t)

	n := alerts.NewNotifier(context.Background(), &config.Config{ResendAPIKey: "re_TESTKEY_never_in_output", AlertEmailTo: "ops@example.com"})

	if isLogOnly(n) {
		t.Fatal("notifier is log-only although RESEND_API_KEY and ALERT_EMAIL_TO are set")
	}
	if strings.Contains(logged.String(), "log-only") || strings.Contains(logged.String(), "TESTKEY") {
		t.Fatalf("startup log warns log-only or carries the key: %s", logged.String())
	}
}

func TestNewNotifier_EmailWinsOverAWebhook(t *testing.T) {
	var posts atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { posts.Add(1) }))
	defer hook.Close()

	n := alerts.NewNotifier(context.Background(), &config.Config{ResendAPIKey: "re_x", AlertEmailTo: "ops@example.com", AlertWebhookURL: hook.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = n.Notify(ctx, monitoring.Alert{Title: "t"})

	if isLogOnly(n) || posts.Load() != 0 {
		t.Fatalf("log-only %v, webhook posts %d: want the email notifier, not the webhook", isLogOnly(n), posts.Load())
	}
}

func TestNewNotifier_NoAlertChannelWarnsOnceThatAlertsAreLogOnly(t *testing.T) {
	logged := captureLogs(t)

	n := alerts.NewNotifier(context.Background(), &config.Config{})

	if !isLogOnly(n) {
		t.Fatal("notifier is real without any alert channel")
	}
	if c := strings.Count(logged.String(), "alerts are log-only"); c != 1 {
		t.Fatalf("log-only warnings = %d, want 1; log: %s", c, logged.String())
	}
	if !strings.Contains(logged.String(), "level=WARN") {
		t.Fatalf("log-only notice is not a Warn: %s", logged.String())
	}
}
