package handler_test

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type failingNotifier struct{}

func (failingNotifier) Notify(context.Context, alert.Alert) error { return errors.New("dead host") }

type alertsBody struct {
	Enabled      bool   `json:"enabled"`
	Paused       bool   `json:"paused"`
	LastNotifyOK bool   `json:"last_notify_ok"`
	Failures     int64  `json:"consecutive_notify_failures"`
	NotifierNop  bool   `json:"notifier_nop"`
	LastPassAt   string `json:"last_pass_at"`
}

func getAlerts(t *testing.T, srv http.Handler) alertsBody {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/alerts", nil))
	var body alertsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func startedAlertServer(t *testing.T, n alert.AlertNotifier) http.Handler {
	t.Helper()
	m := alert.NewMonitor(n, killSwitchLoopInterval, alert.Condition{
		Key: "down",
		Eval: func(context.Context) *alert.Alert {
			return &alert.Alert{Title: "t", Message: "m", Severity: alert.SeveritySignal}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() { cancel(); m.Shutdown(context.Background()) })
	operator := shared.NewUserId(uuid.New())
	return mountAdminHandler(handler.New(nil, nil).WithAlertMonitor(m), operator.String(), operator, true)
}

func TestAlerts_FailingNotifyIsVisible(t *testing.T) {
	srv := startedAlertServer(t, failingNotifier{})
	deadline := time.Now().Add(killSwitchDeadline)
	for getAlerts(t, srv).Failures == 0 {
		if time.Now().After(deadline) {
			t.Fatal("notify failures never surfaced")
		}
		time.Sleep(killSwitchLoopInterval)
	}
	body := getAlerts(t, srv)
	if !body.Enabled || body.Paused || body.LastNotifyOK || body.LastPassAt == "" {
		t.Fatalf("body = %+v", body)
	}
}

func TestAlerts_NopNotifierIsVisible(t *testing.T) {
	srv := startedAlertServer(t, alert.NopNotifier{})
	if body := getAlerts(t, srv); !body.NotifierNop || !body.Enabled {
		t.Fatalf("body = %+v, want notifier_nop", body)
	}
}

type pushBody struct {
	PushConfigured    bool   `json:"push_configured"`
	LastNotifyOKAt    string `json:"last_notify_ok_at"`
	LastNotifyErrorAt string `json:"last_notify_error_at"`
	LastPassAt        string `json:"last_pass_at"`
}

func getPush(t *testing.T, srv http.Handler) pushBody {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/alerts", nil))
	var b pushBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b
}

func TestAlerts_NopNotifierIsNotPushConfigured(t *testing.T) {
	srv := startedAlertServer(t, alert.NopNotifier{})
	deadline := time.Now().Add(killSwitchDeadline)
	for getPush(t, srv).LastPassAt == "" {
		if time.Now().After(deadline) {
			t.Fatal("monitor never completed a pass")
		}
		time.Sleep(killSwitchLoopInterval)
	}
	if b := getPush(t, srv); b.PushConfigured || b.LastNotifyOKAt != "" {
		t.Fatalf("body = %+v, want push_configured false and no success time", b)
	}
}

func TestAlerts_FailedNotifyShowsErrorTime(t *testing.T) {
	srv := startedAlertServer(t, failingNotifier{})
	deadline := time.Now().Add(killSwitchDeadline)
	for getPush(t, srv).LastNotifyErrorAt == "" {
		if time.Now().After(deadline) {
			t.Fatal("error time never surfaced")
		}
		time.Sleep(killSwitchLoopInterval)
	}
	if b := getPush(t, srv); !b.PushConfigured || b.LastNotifyOKAt != "" {
		t.Fatalf("body = %+v", b)
	}
}
