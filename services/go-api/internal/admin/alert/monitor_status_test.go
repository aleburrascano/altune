package alert

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestMonitorStatus_FailingNotifyIsReflected(t *testing.T) {
	firing := true
	m := newTestMonitor(&stubNotifier{err: errors.New("dead host")}, signalCond("k", &firing))
	m.tick(context.Background())
	m.tick(context.Background())

	st := m.Status()
	if st.LastNotifyOK || st.ConsecutiveFailures != 2 || st.LastPass.IsZero() {
		t.Fatalf("status = %+v, want 2 failures, not ok, pass recorded", st)
	}
}

func TestMonitorStatus_SuccessResetsFailures(t *testing.T) {
	firing := true
	m := newTestMonitor(&flakyNotifier{failCalls: 1}, signalCond("k", &firing))
	m.tick(context.Background())
	m.tick(context.Background())

	if st := m.Status(); !st.LastNotifyOK || st.ConsecutiveFailures != 0 {
		t.Fatalf("status = %+v, want recovered", st)
	}
}

func TestMonitorStatus_NopNotifierIsReported(t *testing.T) {
	if !newTestMonitor(NopNotifier{}).Status().NopNotifier {
		t.Fatal("Nop notifier not reported")
	}
	if newTestMonitor(&stubNotifier{}).Status().NopNotifier {
		t.Fatal("real notifier reported as Nop")
	}
}

func TestMonitorStatus_PanicIsCounted(t *testing.T) {
	m := newTestMonitor(NopNotifier{}, Condition{Key: "p", Eval: func(context.Context) *Alert { panic("boom") }})
	m.tick(context.Background())

	if got := m.Status().ContainedPanics; got != 1 {
		t.Fatalf("contained panics = %d, want 1", got)
	}
}

func TestMonitor_SignalPageLogsFiredWithoutBody(t *testing.T) {
	firing := true
	m := newTestMonitor(&stubNotifier{}, signalCond("k", &firing))
	var buf bytes.Buffer
	m.logger = slog.New(slog.NewJSONHandler(&buf, nil))
	m.tick(context.Background())

	out := buf.String()
	if !strings.Contains(out, `"msg":"alert.fired"`) || !strings.Contains(out, `"key":"k"`) {
		t.Fatalf("no alert.fired line: %s", out)
	}
	if strings.Contains(out, "state down") {
		t.Fatalf("alert body leaked into log: %s", out)
	}
}
