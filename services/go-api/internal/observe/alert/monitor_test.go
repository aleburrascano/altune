package alert

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
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

// TestMonitor_BlockingConditionDoesNotFreezeTicker reproduces the per-call
// timeout defect: a condition whose Eval only returns when its context is
// cancelled (a stuck, context-aware dependency) must not hang evaluate, and
// the conditions after it must still run on the same tick.
func TestMonitor_BlockingConditionDoesNotFreezeTicker(t *testing.T) {
	secondRan := false
	blocked := Condition{
		Key: "stuck",
		Eval: func(ctx context.Context) *Alert {
			<-ctx.Done()
			return nil
		},
	}
	second := Condition{
		Key: "next",
		Eval: func(context.Context) *Alert {
			secondRan = true
			return nil
		},
	}
	m := newTestMonitor(&stubNotifier{}, blocked, second)
	m.evalTimeout = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		m.evaluate(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("evaluate hung on a blocking condition; the monitor ticker is frozen")
	}
	if !secondRan {
		t.Fatal("condition after the blocking one never ran")
	}
}

// TestMonitor_PausedTickSkipsEvaluation reproduces the runtime kill-switch gap:
// a paused monitor must skip its per-tick work without a restart, and resume
// evaluating once un-paused.
func TestMonitor_PausedTickSkipsEvaluation(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.Pause()
	m.tick(context.Background())
	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 while paused", n.calls)
	}

	m.Resume()
	m.tick(context.Background())
	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after resume", n.calls)
	}
}

// TestMonitor_NonPositiveIntervalDoesNotPanic reproduces the unclamped-interval
// defect. loop is called directly rather than through Start because the panic
// would otherwise be raised on the spawned goroutine, where no recover can
// reach it and the whole test binary dies with it.
func TestMonitor_NonPositiveIntervalDoesNotPanic(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			m := NewMonitor(&stubNotifier{}, interval)
			canceled, cancel := context.WithCancel(context.Background())
			cancel()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("loop panicked on interval %s: %v", interval, r)
				}
			}()
			m.loop(canceled)
		})
	}
}

// TestMonitor_PanickingConditionDoesNotStopThePass reproduces the missing
// recover: runloop.Spawn has none either, so today this panic unwinds the loop
// goroutine and terminates the process.
func TestMonitor_PanickingConditionDoesNotStopThePass(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	blowsUp := Condition{
		Key:  "boom",
		Eval: func(context.Context) *Alert { panic("condition blew up") },
	}
	m := newTestMonitor(n, blowsUp, signalCond("dep", &firing))

	m.evaluate(context.Background())

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (the condition after a panicking one must still run)", n.calls)
	}
}

// TestMonitor_PanickingConditionHoldsItsPreviousState pins the panic's meaning:
// unknown, not recovered. Reading it as a recovery would clear firing and page
// the same open incident again on the next pass.
func TestMonitor_PanickingConditionHoldsItsPreviousState(t *testing.T) {
	blowsUp := false
	n := &stubNotifier{}
	cond := Condition{
		Key: "dep",
		Eval: func(context.Context) *Alert {
			if blowsUp {
				panic("condition blew up")
			}
			return &Alert{Title: "t", Message: "state down", Severity: SeveritySignal}
		},
	}
	m := newTestMonitor(n, cond)

	m.evaluate(context.Background())
	blowsUp = true
	m.evaluate(context.Background())
	blowsUp = false
	m.evaluate(context.Background())

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (a panic is unknown state, not a recovery)", n.calls)
	}
}

// TestMonitor_PauseMidPassStopsLaterNotifications reproduces the once-per-tick
// pause check: conditions get up to 10s each, so an operator who pauses while
// the first one is still blocked must not be paged by the ones behind it.
func TestMonitor_PauseMidPassStopsLaterNotifications(t *testing.T) {
	var m *Monitor
	firing := true
	n := &stubNotifier{}
	operatorPauses := Condition{
		Key: "slow",
		Eval: func(context.Context) *Alert {
			m.Pause()
			return nil
		},
	}
	m = newTestMonitor(n, operatorPauses, signalCond("dep", &firing))

	m.evaluate(context.Background())

	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 (conditions after a mid-pass Pause must not page)", n.calls)
	}
}

// TestMonitor_ResumeRearmsAnIncidentThatWentUnnotified reproduces the stale
// firing state: the key was marked firing before the pause, nothing was pushed
// during it, so after Resume the still-open incident must page again.
func TestMonitor_ResumeRearmsAnIncidentThatWentUnnotified(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m := newTestMonitor(n, signalCond("dep", &firing))

	m.tick(context.Background())
	m.Pause()
	m.tick(context.Background())
	m.Resume()
	m.tick(context.Background())

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, pause, resume, still firing)", n.calls)
	}
}

func TestNopNotifier(t *testing.T) {
	if err := (NopNotifier{}).Notify(context.Background(), Alert{}); err != nil {
		t.Fatalf("NopNotifier.Notify returned %v, want nil", err)
	}
}

func TestNopNotifier_LogsFiringAlertAtError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := (NopNotifier{}).Notify(context.Background(), Alert{Title: "dep down", Message: "state down"}); err != nil {
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

func TestMonitorStatus_NotifyTimesFollowOutcome(t *testing.T) {
	firing := true
	m := newTestMonitor(&flakyNotifier{failCalls: 1}, signalCond("k", &firing))

	m.tick(context.Background())
	st := m.Status()
	if st.LastNotifyErrorAt.IsZero() || !st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after failure status = %+v, want only error time", st)
	}

	m.tick(context.Background())
	if st := m.Status(); st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after success status = %+v, want ok time", st)
	}
}

func TestMonitorStatus_NopNotifierNeverRecordsSuccess(t *testing.T) {
	firing := true
	m := newTestMonitor(NopNotifier{}, signalCond("k", &firing))
	m.tick(context.Background())

	if st := m.Status(); !st.LastNotifyOKAt.IsZero() || !st.NopNotifier {
		t.Fatalf("status = %+v, nop must not claim a delivered page", st)
	}
}

func TestMonitorStatus_AlwaysFailingNeverRecordsSuccess(t *testing.T) {
	firing := true
	m := newTestMonitor(&stubNotifier{err: errors.New("dead")}, signalCond("k", &firing))
	m.tick(context.Background())
	if !m.Status().LastNotifyOKAt.IsZero() {
		t.Fatal("ok time set despite failure")
	}
}

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
