package handler_test

import (
	"altune/go-api/internal/admin/alert"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
