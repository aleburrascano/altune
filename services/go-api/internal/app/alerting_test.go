package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func captureLeaderLoopLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	return logging.CaptureRing(t, "info")
}

type alwaysLeader struct{}

func (alwaysLeader) Start(context.Context)      {}
func (alwaysLeader) Await(context.Context) bool { return true }
func (alwaysLeader) Shutdown(context.Context)   {}

func (alwaysLeader) Counters() leader.Counters { return leader.Counters{} }

func (alwaysLeader) LeaderContext(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}

func leadAlways(ctx context.Context, t *testing.T, r *jobs.Runner) {
	t.Helper()
	r.Lead(ctx, alwaysLeader{})
	t.Cleanup(r.Wait)
}

func awaitPass(t *testing.T, passed <-chan struct{}) {
	t.Helper()
	select {
	case <-passed:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop never ran a pass")
	}
}

func recordNamed(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	for _, rec := range ring.Snapshot() {
		if rec.Message == msg {
			return rec
		}
	}
	t.Fatalf("no %q record was captured", msg)
	return logging.CapturedRecord{}
}

func TestAlertMonitor_ALogInsideAPassCarriesTheJobAndARunID(t *testing.T) {
	ring := captureLeaderLoopLogs(t)
	passed := make(chan struct{}, 1)
	runner := jobs.Runner{Now: clock.Now, NewTicker: clock.NewTicker}
	condition := monitoring.Condition{Key: "probe", Eval: func(ctx context.Context) *monitoring.Alert {
		slog.InfoContext(ctx, "alert pass line")
		select {
		case passed <- struct{}{}:
		default:
		}
		return nil
	}}
	monitor, err := app.NewAlertMonitor(monitoring.NopNotifier{}, 5*time.Millisecond, []monitoring.Condition{condition})
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runner.Ticker(jobs.AlertMonitor, 5*time.Millisecond, monitor.Pass)
	leadAlways(ctx, t, &runner)
	awaitPass(t, passed)

	rec := recordNamed(t, ring, "alert pass line")
	if rec.Attrs["job"] != string(jobs.AlertMonitor) || rec.Attrs["run_id"] == "" {
		t.Errorf("attrs job=%q run_id=%q, want %q and a run_id", rec.Attrs["job"], rec.Attrs["run_id"], jobs.AlertMonitor)
	}
}

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
