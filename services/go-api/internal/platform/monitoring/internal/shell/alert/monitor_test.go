package alert_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock/clocktest"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const passWait = 2 * time.Second

type stubNotifier struct {
	calls    int
	messages []string
	err      error
}

func (s *stubNotifier) Notify(_ context.Context, a monitoring.Alert) error {
	s.calls++
	s.messages = append(s.messages, a.Message)
	return s.err
}

type flakyNotifier struct {
	calls     int
	failCalls int
}

func (f *flakyNotifier) Notify(context.Context, monitoring.Alert) error {
	f.calls++
	if f.calls <= f.failCalls {
		return errors.New("transient push failure")
	}
	return nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type passes struct {
	begin chan bool
	ended chan struct{}
}

func (p *passes) scope(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	select {
	case leads := <-p.begin:
		if !leads {
			p.ended <- struct{}{}
			return ctx, func() {}, false
		}
		return ctx, func() { p.ended <- struct{}{} }, true
	case <-ctx.Done():
		return ctx, func() {}, false
	}
}

func (p *passes) run(t *testing.T, leads bool) {
	t.Helper()
	select {
	case p.begin <- leads:
	case <-time.After(passWait):
		t.Fatalf("no monitor pass started within %s", passWait)
	}
	select {
	case <-p.ended:
	case <-time.After(passWait):
		t.Fatalf("monitor pass did not finish within %s", passWait)
	}
}

func (p *passes) step(t *testing.T) {
	t.Helper()
	p.run(t, true)
}

func (p *passes) settlePending() {
	select {
	case p.begin <- true:
		<-p.ended
	case <-time.After(100 * time.Millisecond):
	}
}

func (p *passes) assertNoPassStarts(t *testing.T, why string) {
	t.Helper()
	select {
	case p.begin <- true:
		<-p.ended
		t.Fatal(why)
	case <-time.After(100 * time.Millisecond):
	}
}

func startMonitor(t *testing.T, m *monitoring.AlertMonitor) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() {
		stop()
		m.Shutdown(context.Background())
	})
}

func newMonitor(t *testing.T, n monitoring.AlertNotifier, interval time.Duration, conds ...monitoring.Condition) *monitoring.AlertMonitor {
	t.Helper()
	return newMonitorUnder(t, n, interval, background.EveryPassLeads, conds...)
}

func newMonitorUnder(t *testing.T, n monitoring.AlertNotifier, interval time.Duration, scope background.OnlyWhileLeader, conds ...monitoring.Condition) *monitoring.AlertMonitor {
	t.Helper()
	m, err := monitoring.NewAlertMonitor(n, interval, time.Now, scope, conds...)
	if err != nil {
		t.Fatalf("NewAlertMonitor(interval %v): %v", interval, err)
	}
	return m
}

func steppedMonitor(t *testing.T, n monitoring.AlertNotifier, conds ...monitoring.Condition) (*monitoring.AlertMonitor, *passes) {
	t.Helper()
	p := &passes{begin: make(chan bool), ended: make(chan struct{}, 1)}
	m := newMonitorUnder(t, n, time.Millisecond, p.scope, conds...)
	startMonitor(t, m)
	return m, p
}

func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
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

func TestMonitor_PagesWithEveryPassLeadingByDefault(t *testing.T) {
	notified := make(chan struct{}, 1)
	n := notifierFunc(func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	})
	firing := true
	startMonitor(t, newMonitor(t, n, time.Millisecond, signalCond("dep", &firing)))

	select {
	case <-notified:
	case <-time.After(passWait):
		t.Fatalf("a monitor started without a leadership scope never paged within %s", passWait)
	}
}

type notifierFunc func()

func (f notifierFunc) Notify(context.Context, monitoring.Alert) error {
	f()
	return nil
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
			t.Fatalf("%s condition ran with no deadline, so a blocking one would freeze the ticker", name)
		}
		if budget := probe.deadline.Sub(probe.start); budget <= 9*time.Second || budget > 10*time.Second {
			t.Errorf("%s condition deadline %v after its start, want just under 10s", name, budget)
		}
	}
	if !second.deadline.After(first.deadline) {
		t.Errorf("second deadline %v not after first %v: conditions share one pass-wide deadline", second.deadline, first.deadline)
	}
}

func TestMonitor_PausedTickSkipsEvaluation(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m, p := steppedMonitor(t, n, signalCond("dep", &firing))

	m.Pause()
	p.settlePending()
	p.assertNoPassStarts(t, "a pass started while the monitor was paused")
	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 while paused", n.calls)
	}

	m.Resume()
	p.step(t)
	if n.calls != 1 {
		t.Fatalf("notify calls = %d, want 1 after resume", n.calls)
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

func TestMonitor_PauseMidPassStopsLaterNotifications(t *testing.T) {
	var m *monitoring.AlertMonitor
	firing := true
	n := &stubNotifier{}
	operatorPauses := monitoring.Condition{
		Key: "slow",
		Eval: func(context.Context) *monitoring.Alert {
			m.Pause()
			return nil
		},
	}
	m, p := steppedMonitor(t, n, operatorPauses, signalCond("dep", &firing))

	p.step(t)

	if n.calls != 0 {
		t.Fatalf("notify calls = %d, want 0 (conditions after a mid-pass Pause must not page)", n.calls)
	}
}

func TestMonitor_ResumeRearmsAnIncidentThatWentUnnotified(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	m, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.step(t)
	m.Pause()
	p.settlePending()
	m.Resume()
	p.step(t)
	p.step(t)

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, pause, resume, still firing)", n.calls)
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
	if !newMonitor(t, monitoring.NopNotifier{}, time.Second).Status().NopNotifier {
		t.Fatal("Nop notifier not reported")
	}
	if newMonitor(t, &stubNotifier{}, time.Second).Status().NopNotifier {
		t.Fatal("real notifier reported as Nop")
	}
}

func TestNewAlertMonitor_RefusesAnIntervalThatIsNotPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		m, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, interval, time.Now, background.EveryPassLeads)
		if !errors.Is(err, monitoring.ErrInvalidInterval) {
			t.Errorf("NewAlertMonitor(interval %v) error = %v, want ErrInvalidInterval", interval, err)
		}
		if m != nil {
			t.Errorf("NewAlertMonitor(interval %v) returned a monitor with its error", interval)
		}
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

func TestMonitor_LostLeadershipRepagesAnIncidentOnRegain(t *testing.T) {
	firing := true
	n := &stubNotifier{}
	_, p := steppedMonitor(t, n, signalCond("dep", &firing))

	p.run(t, true)
	p.run(t, false)
	p.run(t, true)

	if n.calls != 2 {
		t.Fatalf("notify calls = %d, want 2 (fire, lose term, regain, still firing)", n.calls)
	}
}

type pageLog struct {
	pages    []monitoring.Alert
	failures int
}

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
	_, p := steppedMonitor(t, n, countingCond("dep", &firing))

	p.step(t)
	firing = false
	p.step(t)
	firing = true
	p.step(t)

	assertNewPage(t, n.pages)
}

func TestMonitor_AnUndeliveredFiringThatRecoversIsForgotten(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	_, p := steppedMonitor(t, n, countingCond("dep", &firing))

	p.step(t)
	firing = false
	p.step(t)
	firing = true
	p.step(t)

	assertNewPage(t, n.pages)
}

func TestMonitor_LosingLeadershipForgetsAnUndeliveredPage(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	_, p := steppedMonitor(t, n, countingCond("dep", &firing))

	p.run(t, true)
	p.run(t, false)
	p.run(t, true)

	assertNewPage(t, n.pages)
}

func TestMonitor_ResumeForgetsAnUndeliveredPage(t *testing.T) {
	firing := true
	n := &pageLog{failures: 1}
	m, p := steppedMonitor(t, n, countingCond("dep", &firing))

	p.step(t)
	m.Pause()
	p.settlePending()
	m.Resume()
	p.step(t)

	assertNewPage(t, n.pages)
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

func clockedMonitor(t *testing.T, n monitoring.AlertNotifier, clk *clocktest.Manual, conds ...monitoring.Condition) (*monitoring.AlertMonitor, *passes) {
	t.Helper()
	p := &passes{begin: make(chan bool), ended: make(chan struct{}, 1)}
	m, err := monitoring.NewAlertMonitor(n, time.Millisecond, clk.Func(), p.scope, conds...)
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	startMonitor(t, m)
	return m, p
}

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

func TestNewMonitor_RefusesANilLeadershipScope(t *testing.T) {
	m, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, time.Second, time.Now, nil)

	if !errors.Is(err, monitoring.ErrAlertLeadershipRequired) {
		t.Fatalf("NewAlertMonitor(nil scope) error = %v, want ErrAlertLeadershipRequired", err)
	}
	if m != nil {
		t.Error("NewAlertMonitor returned a monitor with its error")
	}
}

func TestNewMonitor_RefusesANilClock(t *testing.T) {
	m, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, time.Second, nil, background.EveryPassLeads)

	if !errors.Is(err, monitoring.ErrAlertClockRequired) {
		t.Fatalf("NewAlertMonitor(nil clock) error = %v, want ErrAlertClockRequired", err)
	}
	if m != nil {
		t.Error("NewAlertMonitor returned a monitor with its error")
	}
}
