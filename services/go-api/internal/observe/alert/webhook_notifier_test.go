package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookNotifier_PostsAlertJSON(t *testing.T) {
	var got webhookPayload
	var method, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, contentType = r.Method, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	err := NewWebhookNotifier(srv.URL).Notify(context.Background(), Alert{Title: "t", Message: "m", Severity: SeveritySignal})
	if err != nil {
		t.Fatalf("Notify = %v, want nil", err)
	}
	if method != http.MethodPost || contentType != "application/json" {
		t.Errorf("method %q content-type %q", method, contentType)
	}
	if got.Title != "t" || got.Message != "m" || got.Severity != int(SeveritySignal) {
		t.Errorf("payload = %+v", got)
	}
}

func TestWebhookNotifier_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := NewWebhookNotifier(srv.URL).Notify(context.Background(), Alert{}); err == nil {
		t.Fatal("Notify on 500 = nil, want error")
	}
}

func TestWebhookNotifier_ErrorsWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := NewWebhookNotifier(url).Notify(context.Background(), Alert{}); err == nil {
		t.Fatal("Notify to a closed server = nil, want error")
	}
}

func TestWebhookNotifier_ErrorNeverContainsWebhookURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hookURL := srv.URL + "/hook/SECRET123"
	srv.Close()

	err := NewWebhookNotifier(hookURL).Notify(context.Background(), Alert{})
	if err == nil {
		t.Fatal("Notify to a closed server = nil, want error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("webhook URL leaked into error: %v", err)
	}
}

func TestWebhookNotifier_BadURLErrorNeverContainsWebhookURL(t *testing.T) {
	err := NewWebhookNotifier("http://host/hook/SECRET123\x7f").Notify(context.Background(), Alert{})
	if err == nil {
		t.Fatal("Notify to a malformed URL = nil, want error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("webhook URL leaked into error: %v", err)
	}
}
