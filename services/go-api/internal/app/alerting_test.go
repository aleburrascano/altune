package app_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func alertMonitorUsesNopNotifier(t *testing.T, h http.Handler, token string) bool {
	t.Helper()
	code, body := getAs(t, h, "/observe/metrics/live", token)
	if code != http.StatusOK {
		t.Fatalf("operator read of /observe/metrics/live = %d, want 200; body %s", code, body)
	}
	var got struct {
		AlertMonitor *struct {
			NopNotifier bool `json:"nop_notifier"`
		} `json:"alert_monitor"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.AlertMonitor == nil {
		t.Fatalf("live metrics carry no alert_monitor: %s", body)
	}
	return got.AlertMonitor.NopNotifier
}

func TestAlerting_TheMonitorSendsThroughTheConfiguredChannel(t *testing.T) {
	cases := map[string]struct {
		webhook string
		nop     bool
	}{
		"a webhook URL builds a real notifier":    {"http://127.0.0.1:1/alerts", false},
		"no alert channel leaves alerts log-only": {"", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newJWKSFixture(t)
			f.cfg.AlertWebhookURL = tc.webhook
			h := assembledHandler(t, f.cfg)

			if got := alertMonitorUsesNopNotifier(t, h, signedToken(t, f.key, f.operator)); got != tc.nop {
				t.Errorf("alert_monitor.nop_notifier = %v, want %v", got, tc.nop)
			}
		})
	}
}
