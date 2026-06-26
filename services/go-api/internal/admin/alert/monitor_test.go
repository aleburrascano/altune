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

// newTestMonitor builds a monitor without starting its ticker, so tests can
// drive evaluate() deterministically.
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
	m.evaluate(context.Background()) // still firing — must not page again

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (once per incident)", n.calls)
	}
}

func TestMonitor_RefiresAfterRecovery(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.evaluate(context.Background()) // fire
	firing = false
	m.evaluate(context.Background()) // recover
	firing = true
	m.evaluate(context.Background()) // new incident — fires again

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

	// Must not panic; the monitor logs and continues.
	m.evaluate(context.Background())

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1", n.calls)
	}
}

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), Alert{}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}
}
