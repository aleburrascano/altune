package notifier_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNopNotifier(t *testing.T) {
	if err := (notifier.NopNotifier{}).Notify(context.Background(), domain.Alert{}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}
}

func TestNopNotifier_LogsFiringAlertAtError(t *testing.T) {
	buf := logging.CaptureText(t, slog.LevelInfo)

	if err := (notifier.NopNotifier{}).Notify(context.Background(), domain.Alert{Title: "dep down", Message: "state down"}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("log = %q, want an ERROR-level line", out)
	}
	if !strings.Contains(out, "dep down") || !strings.Contains(out, "state down") {
		t.Fatalf("log = %q, want it to carry the alert title and message", out)
	}
}

func TestWebhookNotifier_PostsAlertJSON(t *testing.T) {
	var got struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Severity int    `json:"severity"`
	}
	var method, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, contentType = r.Method, r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("webhook body is not the alert JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	err := notifier.NewWebhookNotifier(srv.URL).Notify(context.Background(), domain.Alert{Title: "t", Message: "m", Severity: domain.SeverityWatch})
	if err != nil {
		t.Fatalf("Notify = %v, want nil", err)
	}
	if method != http.MethodPost || contentType != "application/json" {
		t.Errorf("method %q content-type %q", method, contentType)
	}
	if got.Title != "t" || got.Message != "m" || got.Severity != int(domain.SeverityWatch) {
		t.Errorf("payload = %+v", got)
	}
}

func TestWebhookNotifier_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := notifier.NewWebhookNotifier(srv.URL).Notify(context.Background(), domain.Alert{}); err == nil {
		t.Fatal("Notify on 500 = nil, want error")
	}
}

func TestWebhookNotifier_ErrorsWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := notifier.NewWebhookNotifier(url).Notify(context.Background(), domain.Alert{}); err == nil {
		t.Fatal("Notify to a closed server = nil, want error")
	}
}

func TestWebhookNotifier_ErrorNeverContainsWebhookURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hookURL := srv.URL + "/hook/SECRET123"
	srv.Close()

	err := notifier.NewWebhookNotifier(hookURL).Notify(context.Background(), domain.Alert{})
	if err == nil {
		t.Fatal("Notify to a closed server = nil, want error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("webhook URL leaked into error: %v", err)
	}
}

func TestWebhookNotifier_BadURLErrorNeverContainsWebhookURL(t *testing.T) {
	err := notifier.NewWebhookNotifier("http://host/hook/SECRET123\x7f").Notify(context.Background(), domain.Alert{})
	if err == nil {
		t.Fatal("Notify to a malformed URL = nil, want error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("webhook URL leaked into error: %v", err)
	}
}
