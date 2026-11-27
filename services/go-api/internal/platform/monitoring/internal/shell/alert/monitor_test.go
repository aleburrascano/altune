package alert_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testInterval = time.Second

type stubNotifier struct {
	calls    int
	messages []string
	err      error
}

func (s *stubNotifier) Silent() bool { return false }

func (s *stubNotifier) Notify(_ context.Context, a monitoring.Alert) error {
	s.calls++
	s.messages = append(s.messages, a.Message)
	return s.err
}

type flakyNotifier struct {
	calls     int
	failCalls int
}

func (f *flakyNotifier) Silent() bool { return false }

func (f *flakyNotifier) Notify(context.Context, monitoring.Alert) error {
	f.calls++
	if f.calls <= f.failCalls {
		return errors.New("transient push failure")
	}
	return nil
}

type passes struct {
	m *monitoring.AlertMonitor
}

func (p passes) step(t *testing.T) {
	t.Helper()
	if err := p.m.Pass(t.Context()); err != nil {
		t.Fatalf("Pass: %v", err)
	}
}

func newMonitor(t *testing.T, n monitoring.AlertNotifier, interval time.Duration, now clock.Func, conds ...monitoring.Condition) *monitoring.AlertMonitor {
	t.Helper()
	m, err := monitoring.NewAlertMonitor(n, interval, now, conds...)
	if err != nil {
		t.Fatalf("NewAlertMonitor(interval %v): %v", interval, err)
	}
	return m
}

func steppedMonitor(t *testing.T, n monitoring.AlertNotifier, conds ...monitoring.Condition) (*monitoring.AlertMonitor, passes) {
	t.Helper()
	m := newMonitor(t, n, testInterval, time.Now, conds...)
	return m, passes{m: m}
}

func clockedMonitor(t *testing.T, n monitoring.AlertNotifier, clk *clocktest.Manual, conds ...monitoring.Condition) (*monitoring.AlertMonitor, passes) {
	t.Helper()
	m := newMonitor(t, n, testInterval, clk.Func(), conds...)
	return m, passes{m: m}
}

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureJSON(t, slog.LevelInfo)
}

func signalCond(key string, firing *bool) monitoring.Condition {
	return monitoring.Condition{
		Key: key,
		Eval: func(context.Context) *monitoring.Alert {
			if *firing {
				return &monitoring.Alert{Title: "t", Message: "state down", Severity: monitoring.SeverityWatch}
			}
			return nil
		},
	}
}

func TestMonitor_SignalFiresOnce(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	_, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.step(t)
	p.step(t)

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (once per incident)", n.calls)
	}
	if n.messages[0] != "state down" {
		t.Errorf("notified message = %q, want the alert's message", n.messages[0])
	}
}

func TestMonitor_RefiresAfterRecovery(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	_, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.step(t)
	firing = false
	p.step(t)
	firing = true
	p.step(t)

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, recover, fire)", n.calls)
	}
}

func TestMonitor_NonSignalDoesNotPage(t *testing.T) {
	n := &stubNotifier{}
	cond := monitoring.Condition{
		Key: "log-only",
		Eval: func(context.Context) *monitoring.Alert {
			return &monitoring.Alert{Title: "t", Message: "m"}
		},
	}
	_, p := steppedMonitor(t, n, cond)

	p.step(t)

	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 (only the signal tier pages)", n.calls)
	}
}

func TestMonitor_NotifierFailureDoesNotPanic(t *testing.T) {
	firing := true
	n := &stubNotifier{err: errors.New("push failed")}
	_, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.step(t)

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1", n.calls)
	}
}

func TestMonitor_RetriesAfterFailedNotify(t *testing.T) {
	firing := true
	n := &flakyNotifier{failCalls: 1}
	_, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.step(t)
	p.step(t)

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (failed push must re-arm, not permanently silence)", n.calls)
	}
}

type deadlineProbe struct {
	start, deadline time.Time
	hasDeadline     bool
}

func (d *deadlineProbe) condition(key string) monitoring.Condition {
	return monitoring.Condition{
		Key: key,
		Eval: func(ctx context.Context) *monitoring.Alert {
			d.start = time.Now()
			d.deadline, d.hasDeadline = ctx.Deadline()
			for !time.Now().After(d.start) {
				runtime.Gosched()
			}
			return nil
		},
	}
}

func TestMonitor_EachConditionRunsUnderItsOwnTenSecondDeadline(t *testing.T) {
	var first, second deadlineProbe
	_, p := steppedMonitor(t, &stubNotifier{}, first.condition("first"), second.condition("second"))

	p.step(t)

	for name, probe := range map[string]deadlineProbe{"first": first, "second": second} {
		if !probe.hasDeadline {
			t.Fatalf("%s condition ran with no deadline, so a blocking one would freeze the pass", name)
		}
		if budget := probe.deadline.Sub(probe.start); budget <= 9*time.Second || budget > 10*time.Second {
			t.Errorf("%s condition deadline %v after its start, want just under 10s", name, budget)
		}
	}
	if !second.deadline.After(first.deadline) {
		t.Errorf("second deadline %v not after first %v: conditions share one pass-wide deadline", second.deadline, first.deadline)
	}
}

func TestMonitor_PanickingConditionDoesNotStopThePass(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	blowsUp := monitoring.Condition{
		Key:  "boom",
		Eval: func(context.Context) *monitoring.Alert { panic("condition blew up") },
	}
	_, p := steppedMonitor(t, n, blowsUp, signalCond("dep", &firing))

	p.step(t)

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (the condition after a panicking one must still run)", n.calls)
	}
}

func TestMonitor_PanickingConditionHoldsItsPreviousState(t *testing.T) {
	blowsUp := false
	n := &stubNotifier{}
	cond := monitoring.Condition{
		Key: "dep",
		Eval: func(context.Context) *monitoring.Alert {
			if blowsUp {
				panic("condition blew up")
			}
			return &monitoring.Alert{Title: "t", Message: "state down", Severity: monitoring.SeverityWatch}
		},
	}
	_, p := steppedMonitor(t, n, cond)

	p.step(t)
	blowsUp = true
	p.step(t)
	blowsUp = false
	p.step(t)

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (a panic is unknown state, not a recovery)", n.calls)
	}
}

func TestMonitor_ACanceledPassStopsLaterNotifications(t *testing.T) {
	var cancel context.CancelFunc
	firing := true
	n := &stubNotifier{}
	cancelsThePass := monitoring.Condition{
		Key: "slow",
		Eval: func(context.Context) *monitoring.Alert {
			cancel()
			return nil
		},
	}
	m, _ := steppedMonitor(t, n, cancelsThePass, signalCond("dep", &firing))
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	cancel = stop

	if err := m.Pass(ctx); err != nil {
		t.Fatalf("Pass: %v", err)
	}

	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 (conditions after a canceled pass must not page)", n.calls)
	}
}

func TestMonitorStatus_NotifyTimesFollowOutcome(t *testing.T) {
	firing := true
	m, p := steppedMonitor(t, &flakyNotifier{failCalls: 1}, signalCond("k", &firing))

	p.step(t)
	st := m.Status()
	if st.LastNotifyErrorAt.IsZero() || !st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after failure status = %+v, want only error time", st)
	}

	p.step(t)
	if st := m.Status(); st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after success status = %+v, want ok time", st)
	}
}

func TestMonitorStatus_NopNotifierNeverRecordsSuccess(t *testing.T) {
	firing := true
	m, p := steppedMonitor(t, monitoring.NopNotifier{}, signalCond("k", &firing))

	p.step(t)

	if st := m.Status(); !st.LastNotifyOKAt.IsZero() || !st.NopNotifier {
		t.Fatalf("status = %+v, nop must not claim a delivered page", st)
	}
}

func TestMonitorStatus_AlwaysFailingNeverRecordsSuccess(t *testing.T) {
	firing := true
	m, p := steppedMonitor(t, &stubNotifier{err: errors.New("dead")}, signalCond("k", &firing))

	p.step(t)

	if !m.Status().LastNotifyOKAt.IsZero() {
		t.Fatal("ok time set despite failure")
	}
}

func TestMonitorStatus_FailingNotifyIsReflected(t *testing.T) {
	firing := true
	m, p := steppedMonitor(t, &stubNotifier{err: errors.New("dead host")}, signalCond("k", &firing))

	p.step(t)
	p.step(t)

	st := m.Status()
	if st.LastNotifyOK || st.ConsecutiveFailures != 2 || st.LastPass.IsZero() {
		t.Fatalf("status = %+v, want 2 failures, not ok, pass recorded", st)
	}
}

func TestMonitorStatus_SuccessResetsFailures(t *testing.T) {
	firing := true
	m, p := steppedMonitor(t, &flakyNotifier{failCalls: 1}, signalCond("k", &firing))

	p.step(t)
	p.step(t)

	if st := m.Status(); !st.LastNotifyOK || st.ConsecutiveFailures != 0 {
		t.Fatalf("status = %+v, want recovered", st)
	}
}

func TestMonitorStatus_NopNotifierIsReported(t *testing.T) {
	nop, _ := steppedMonitor(t, monitoring.NopNotifier{})
	if !nop.Status().NopNotifier {
		t.Fatal("Nop notifier not reported")
	}
	real, _ := steppedMonitor(t, &stubNotifier{})
	if real.Status().NopNotifier {
		t.Fatal("real notifier reported as Nop")
	}
}

func TestNewAlertMonitor_RefusesAnIntervalThatIsNotPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		m, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, interval, time.Now)
		if !errors.Is(err, monitoring.ErrInvalidInterval) {
			t.Errorf("NewAlertMonitor(interval %v) error = %v, want ErrInvalidInterval", interval, err)
		}
		if m != nil {
			t.Errorf("NewAlertMonitor(interval %v) returned a monitor with its error", interval)
		}
	}
}

func TestNewMonitor_RefusesANilClock(t *testing.T) {
	m, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, time.Second, nil)

	if !errors.Is(err, monitoring.ErrAlertClockRequired) {
		t.Fatalf("NewAlertMonitor(nil clock) error = %v, want ErrAlertClockRequired", err)
	}
	if m != nil {
		t.Error("NewAlertMonitor returned a monitor with its error")
	}
}

func TestMonitorStatus_PanicIsCounted(t *testing.T) {
	boom := monitoring.Condition{Key: "p", Eval: func(context.Context) *monitoring.Alert { panic("boom") }}
	m, p := steppedMonitor(t, monitoring.NopNotifier{}, boom)

	p.step(t)

	if got := m.Status().ContainedPanics; got != 1 {
		t.Fatalf("contained panics = %d, want 1", got)
	}
}

func TestMonitor_SignalPageLogsFiredWithoutBody(t *testing.T) {
	logs := captureLogs(t)
	firing := true
	_, p := steppedMonitor(t, &stubNotifier{}, signalCond("k", &firing))

	p.step(t)

	out := logs.String()
	if !strings.Contains(out, `"msg":"alert.fired"`) || !strings.Contains(out, `"key":"k"`) {
		t.Fatalf("no alert.fired line: %s", out)
	}
	if strings.Contains(out, "state down") {
		t.Fatalf("alert body leaked into log: %s", out)
	}
}

func TestMonitor_NotifyFailedLogNeverContainsWebhookURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hookURL := srv.URL + "/hook/SECRET123"
	srv.Close()
	logs := captureLogs(t)
	firing := true
	_, p := steppedMonitor(t, monitoring.NewWebhookNotifier(hookURL), signalCond("k", &firing))

	p.step(t)

	out := logs.String()
	if !strings.Contains(out, `"msg":"alert.notify_failed"`) {
		t.Fatalf("no alert.notify_failed line: %s", out)
	}
	if strings.Contains(out, "SECRET123") {
		t.Fatalf("webhook URL leaked into log: %s", out)
	}
}

type pageLog struct {
	pages    []monitoring.Alert
	failures int
}

func (l *pageLog) Silent() bool { return false }

func (l *pageLog) Notify(_ context.Context, a monitoring.Alert) error {
	l.pages = append(l.pages, a)
	if len(l.pages) <= l.failures {
		return errors.New("transient push failure")
	}
	return nil
}

func countingCond(key string, firing *bool) monitoring.Condition {
	evals := 0
	return monitoring.Condition{
		Key: key,
		Eval: func(context.Context) *monitoring.Alert {
			if !*firing {
				return nil
			}
			evals++
			return &monitoring.Alert{Title: "t", Message: fmt.Sprintf("seen %d times", evals), Severity: monitoring.SeverityWatch}
		},
	}
}

func TestMonitor_RetriesOfOneFiringResendTheFirstPageWithItsKeyAndFiringTime(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	_, p := steppedMonitor(t, n, countingCond("dep", &firing))

	before := time.Now()
	p.step(t)
	after := time.Now()
	p.step(t)

	if len(n.pages) != 2 {
		t.Fatalf("pages = %d, want 2 (a failed page is retried)", len(n.pages))
	}
	first := n.pages[0]
	if first.Key != "dep" || first.Message != "seen 1 times" {
		t.Errorf("first page = %+v, want key dep and the first evaluation's message", first)
	}
	if first.FiredAt.Before(before) || first.FiredAt.After(after) {
		t.Errorf("FiredAt = %s, want the pass that first saw the firing (%s to %s)", first.FiredAt, before, after)
	}
	if !reflect.DeepEqual(n.pages[1], first) {
		t.Errorf("retry = %+v, want the first page unchanged %+v", n.pages[1], first)
	}
}

func TestMonitor_AFiringAfterADeliveredRecoveryIsANewPage(t *testing.T) {
	firing := true
	n := &pageLog{}
	clk := clocktest.NewManual(monitorEpoch)
	_, p := clockedMonitor(t, n, clk, countingCond("dep", &firing))

	p.step(t)
	firing = false
	p.step(t)
	firing = true
	clk.Advance(time.Millisecond)
	p.step(t)

	assertNewPage(t, n.pages)
}

func TestMonitor_AnUndeliveredFiringThatRecoversIsForgotten(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	clk := clocktest.NewManual(monitorEpoch)
	_, p := clockedMonitor(t, n, clk, countingCond("dep", &firing))

	p.step(t)
	firing = false
	p.step(t)
	firing = true
	clk.Advance(time.Millisecond)
	p.step(t)

	assertNewPage(t, n.pages)
}

func TestMonitor_APassAfterALongGapForgetsAnUndeliveredPage(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	clk := clocktest.NewManual(monitorEpoch)
	_, p := clockedMonitor(t, n, clk, countingCond("dep", &firing))

	p.step(t)
	clk.Advance(3 * testInterval)
	p.step(t)

	assertNewPage(t, n.pages)
}

func TestMonitor_APassAfterALongGapRepagesAStillFiringIncident(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	clk := clocktest.NewManual(monitorEpoch)
	_, p := clockedMonitor(t, n, clk, signalCond("dep", &firing))

	p.step(t)
	clk.Advance(3 * testInterval)
	p.step(t)

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, a gap long enough to have lost the term, still firing)", n.calls)
	}
}

func TestMonitor_PassesOneIntervalApartKeepTheIncident(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	clk := clocktest.NewManual(monitorEpoch)
	_, p := clockedMonitor(t, n, clk, signalCond("dep", &firing))

	p.step(t)
	clk.Advance(testInterval)
	p.step(t)
	clk.Advance(testInterval)
	p.step(t)

	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (regular passes are one term)", n.calls)
	}
}

func assertNewPage(t *testing.T, pages []monitoring.Alert) {
	t.Helper()
	if len(pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(pages))
	}
	if !pages[1].FiredAt.After(pages[0].FiredAt) {
		t.Errorf("second page FiredAt %s is not after the first %s, so it would share its idempotency key", pages[1].FiredAt, pages[0].FiredAt)
	}
	if pages[1].Message != "seen 2 times" {
		t.Errorf("second page message = %q, want the new evaluation's %q", pages[1].Message, "seen 2 times")
	}
}

var monitorEpoch = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func TestMonitorClock_PageCarriesTheInjectedTimeAsFiredAt(t *testing.T) {
	firing := true
	log := &pageLog{}
	_, p := clockedMonitor(t, log, clocktest.NewManual(monitorEpoch), signalCond("k", &firing))

	p.step(t)

	if len(log.pages) != 1 || !log.pages[0].FiredAt.Equal(monitorEpoch) {
		t.Fatalf("pages = %+v, want one page fired at %v", log.pages, monitorEpoch)
	}
}

func TestMonitorClock_StatusTimesFollowTheInjectedClock(t *testing.T) {
	firing := true
	clk := clocktest.NewManual(monitorEpoch)
	m, p := clockedMonitor(t, &pageLog{failures: 1}, clk, signalCond("k", &firing))

	p.step(t)
	failedAt := clk.Now()
	clk.Advance(time.Hour)
	p.step(t)

	st := m.Status()
	if !st.LastNotifyErrorAt.Equal(failedAt) {
		t.Errorf("LastNotifyErrorAt = %v, want %v", st.LastNotifyErrorAt, failedAt)
	}
	if want := failedAt.Add(time.Hour); !st.LastNotifyOKAt.Equal(want) {
		t.Errorf("LastNotifyOKAt = %v, want %v", st.LastNotifyOKAt, want)
	}
	if want := failedAt.Add(time.Hour); !st.LastPass.Equal(want) {
		t.Errorf("LastPass = %v, want %v", st.LastPass, want)
	}
}
