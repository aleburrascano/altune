package alert

import (
	"context"
	"errors"
	"testing"
)

type stubNotifier struct {
	calls    int
	messages []string
	err      error
}

func (s *stubNotifier) Notify(_ context.Context, a Alert) error {
	s.calls++
	s.messages = append(s.messages, a.Message)
	return s.err
}

type flakyNotifier struct {
	calls     int
	failCalls int
}

func (f *flakyNotifier) Notify(context.Context, Alert) error {
	f.calls++
	if f.calls <= f.failCalls {
		return errors.New("transient push failure")
	}
	return nil
}

func newTestMonitor(n AlertNotifier, conds ...Condition) *Monitor {
	m := NewMonitor(n, 0, conds...)
	return m
}

func signalCond(key string, firing *bool) Condition {
	return Condition{
		Key: key,
		Eval: func(context.Context) *Alert {
			if *firing {
				return &Alert{Title: "t", Message: "state down", Severity: SeveritySignal}
			}
			return nil
		},
	}
}

func TestMonitor_SignalFiresOnce(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.evaluate(context.Background())
	m.evaluate(context.Background())

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (once per incident)", n.calls)
	}
}

func TestMonitor_RefiresAfterRecovery(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.evaluate(context.Background())
	firing = false
	m.evaluate(context.Background())
	firing = true
	m.evaluate(context.Background())

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, recover, fire)", n.calls)
	}
}

func TestMonitor_NonSignalDoesNotPage(t *testing.T) {
	n := &stubNotifier{}
	cond := Condition{
		Key: "log-only",
		Eval: func(context.Context) *Alert {
			return &Alert{Title: "t", Message: "m", Severity: SeverityLog}
		},
	}
	m := newTestMonitor(n, cond)

	m.evaluate(context.Background())

	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 (Log tier must not page)", n.calls)
	}
}

func TestMonitor_NotifierFailureDoesNotPanic(t *testing.T) {
	firing := true
	n := &stubNotifier{err: errors.New("push failed")}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.evaluate(context.Background())

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1", n.calls)
	}
}

func TestMonitor_RetriesAfterFailedNotify(t *testing.T) {
	firing := true
	// Fails on the first push, succeeds thereafter (transient outage).
	n := &flakyNotifier{failCalls: 1}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.evaluate(context.Background()) // fires, notify fails
	m.evaluate(context.Background()) // still firing, must re-attempt

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (failed push must re-arm, not permanently silence)", n.calls)
	}
}

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), Alert{}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}
}
