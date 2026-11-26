package searchqualitycheck_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const runWait = 2 * time.Second

type runs struct {
	begin chan bool
	ended chan struct{}
}

func (r *runs) scope(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	select {
	case cancelTerm := <-r.begin:
		termCtx, cancel := context.WithCancel(ctx)
		if cancelTerm {
			cancel()
		}
		return termCtx, func() {
			cancel()
			r.ended <- struct{}{}
		}, true
	case <-ctx.Done():
		return ctx, func() {}, false
	}
}

func (r *runs) start(t *testing.T, cancelTerm bool) {
	t.Helper()
	select {
	case r.begin <- cancelTerm:
	case <-time.After(runWait):
		t.Fatalf("no eval tick started within %s", runWait)
	}
	select {
	case <-r.ended:
	case <-time.After(runWait):
		t.Fatalf("eval tick did not finish within %s", runWait)
	}
}

func (r *runs) step(t *testing.T) {
	t.Helper()
	r.start(t, false)
}

func (r *runs) assertNoTickStarts(t *testing.T, why string) {
	t.Helper()
	select {
	case r.begin <- false:
		<-r.ended
		t.Fatal(why)
	case <-time.After(100 * time.Millisecond):
	}
}

func mustMeter(t *testing.T, interval time.Duration, runner monitoring.EvalMeterRunner, scope background.OnlyWhileLeader) *monitoring.EvalMeter {
	t.Helper()
	m, err := monitoring.NewEvalMeter(interval, clock.Now, clock.NewTicker, runner, scope)
	if err != nil {
		t.Fatalf("NewEvalMeter: %v", err)
	}
	return m
}

func newMeter(t *testing.T, runner monitoring.EvalMeterRunner) (*monitoring.EvalMeter, *runs) {
	t.Helper()
	r := &runs{begin: make(chan bool), ended: make(chan struct{}, 1)}
	return mustMeter(t, time.Millisecond, runner, r.scope), r
}

func start(t *testing.T, m *monitoring.EvalMeter) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() {
		stop()
		m.Shutdown(context.Background())
	})
}

func steppedMeter(t *testing.T, runner monitoring.EvalMeterRunner) (*monitoring.EvalMeter, *runs) {
	t.Helper()
	m, r := newMeter(t, runner)
	start(t, m)
	return m, r
}

func returning(res monitoring.EvalMeterResult, err error) monitoring.EvalMeterRunner {
	return func(context.Context) (monitoring.EvalMeterResult, error) { return res, err }
}

func wireState(t *testing.T, m *monitoring.EvalMeter) string {
	t.Helper()
	raw, err := json.Marshal(m.Status())
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return body.State
}

func meterAfterOneRun(t *testing.T, res monitoring.EvalMeterResult, err error) *monitoring.EvalMeter {
	t.Helper()
	m, r := steppedMeter(t, returning(res, err))
	r.step(t)
	return m
}

func TestStatus_MarshalsEachStateAsItsWireString(t *testing.T) {
	meters := map[string]*monitoring.EvalMeter{
		"no_data":    mustMeter(t, 0, returning(monitoring.EvalMeterResult{}, nil), background.EveryPassLeads),
		"ok":         meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.9, Baseline: 0.8}, nil),
		"regression": meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.7, Baseline: 0.8, Regressed: true}, nil),
		"error":      meterAfterOneRun(t, monitoring.EvalMeterResult{}, errors.New("provider unreachable")),
	}

	for want, m := range meters {
		if got := wireState(t, m); got != want {
			t.Errorf("state = %q, want %q", got, want)
		}
	}
}

func TestMeter_IsAlwaysEnabled(t *testing.T) {
	for name, m := range map[string]*monitoring.EvalMeter{
		"with a runner": mustMeter(t, 0, returning(monitoring.EvalMeterResult{}, nil), background.EveryPassLeads),
	} {
		if st := m.Status(); !st.Enabled || st.State == "disabled" {
			t.Errorf("%s: status = %+v, want enabled and never disabled", name, st)
		}
	}
}

func TestMeter_NoDataBeforeFirstRun(t *testing.T) {
	m := mustMeter(t, 0, returning(monitoring.EvalMeterResult{}, nil), background.EveryPassLeads)
	if st := m.Status(); st.State != "no_data" {
		t.Fatalf("state = %q, want no_data before any run", st.State)
	}
}

func TestMeter_OkAndRegression(t *testing.T) {
	m := meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.81, Baseline: 0.80}, nil)
	st := m.Status()
	if st.State != "ok" || st.Score == nil || *st.Score != 0.81 || st.Baseline == nil || *st.Baseline != 0.80 {
		t.Fatalf("status = %+v, want ok with score 0.81 and baseline 0.80", st)
	}
	if st.LastRun == nil {
		t.Errorf("status = %+v, want the last run time", st)
	}

	m2 := meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.70, Baseline: 0.80, Regressed: true}, nil)
	if st := m2.Status(); st.State != "regression" {
		t.Fatalf("state = %q, want regression", st.State)
	}
}

func TestMeter_StatusCarriesTheQueries(t *testing.T) {
	queries := []monitoring.EvalMeterQueryResult{{Query: "q", Expect: "e", Passed: true, Position: 1}}
	m := meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.9, Baseline: 0.8, Queries: queries}, nil)

	if got := m.Status().Queries; len(got) != 1 || got[0] != queries[0] {
		t.Errorf("queries = %+v, want %+v", got, queries)
	}
}

func TestMeter_ErrorState(t *testing.T) {
	m := meterAfterOneRun(t, monitoring.EvalMeterResult{}, errors.New("provider unreachable"))
	if st := m.Status(); st.State != "error" || st.Error != "provider unreachable" || st.LastRun == nil {
		t.Fatalf("status = %+v, want error state with the message and run time", st)
	}
}

func TestMeter_StatusSurfacesErroredQueryCount(t *testing.T) {
	m := meterAfterOneRun(t, monitoring.EvalMeterResult{Score: 0.60, Baseline: 0.80, Errored: 2}, nil)

	st := m.Status()
	if st.Errored != 2 {
		t.Errorf("Errored = %d, want 2", st.Errored)
	}
	if st.State == "regression" {
		t.Error("a sub-baseline score with errored queries must not read as a regression")
	}
}

func TestMeter_RunnerRunsUnderAFiveMinuteDeadline(t *testing.T) {
	var remaining time.Duration
	var hasDeadline bool
	m, r := steppedMeter(t, func(ctx context.Context) (monitoring.EvalMeterResult, error) {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		remaining = time.Until(deadline)
		return monitoring.EvalMeterResult{}, context.DeadlineExceeded
	})

	r.step(t)

	if !hasDeadline {
		t.Fatal("runner ran with no deadline, so a blocking runner would stall the scheduler")
	}
	if remaining <= 4*time.Minute || remaining > 5*time.Minute {
		t.Errorf("runner deadline %v away, want just under 5m", remaining)
	}
	if st := m.Status(); st.State != "error" {
		t.Fatalf("state = %q, want error after a runner timeout", st.State)
	}
}

func TestMeter_PausedTickSkipsRun(t *testing.T) {
	calls := 0
	m, r := newMeter(t, func(context.Context) (monitoring.EvalMeterResult, error) {
		calls++
		return monitoring.EvalMeterResult{Score: 0.9, Baseline: 0.8}, nil
	})

	m.Pause()
	start(t, m)
	r.assertNoTickStarts(t, "an eval tick started while the meter was paused")
	if !m.Status().Paused || calls != 0 {
		t.Fatalf("paused = %v, runner calls = %d, want paused with no runs", m.Status().Paused, calls)
	}

	m.Resume()
	r.step(t)
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1 after resume", calls)
	}
}

func TestMeter_PanickingRunnerSurfacesAsError(t *testing.T) {
	m, r := steppedMeter(t, func(context.Context) (monitoring.EvalMeterResult, error) {
		panic("scorer dereferenced a nil provider")
	})

	r.step(t)

	st := m.Status()
	if st.State != "error" {
		t.Fatalf("state = %q, want error after a panicking runner", st.State)
	}
	if !strings.Contains(st.Error, "scorer dereferenced a nil provider") {
		t.Errorf("error = %q, want the panic value in it", st.Error)
	}
}

func TestMeter_APanickingRunIsLoggedOnceByTheGuard(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("error")
	_, r := steppedMeter(t, func(context.Context) (monitoring.EvalMeterResult, error) {
		panic("scorer dereferenced a nil provider")
	})

	r.step(t)

	var messages []string
	for _, rec := range ring.Snapshot() {
		if rec.Level == slog.LevelError.String() {
			messages = append(messages, rec.Message)
		}
	}
	if len(messages) != 1 || messages[0] != "observe.eval_run_panicked" {
		t.Errorf("error records = %v, want only observe.eval_run_panicked", messages)
	}
}

func TestMeter_AFailedRunIsLogged(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("error")
	_, r := steppedMeter(t, func(context.Context) (monitoring.EvalMeterResult, error) {
		return monitoring.EvalMeterResult{}, errors.New("golden set unreadable")
	})

	r.step(t)

	found := false
	for _, rec := range ring.Snapshot() {
		found = found || rec.Message == "observe.eval_run_failed"
	}
	if !found {
		t.Error("no observe.eval_run_failed record for a run that returned an error")
	}
}

func TestMeter_RunAfterAPanicExecutes(t *testing.T) {
	calls := 0
	m, r := steppedMeter(t, func(context.Context) (monitoring.EvalMeterResult, error) {
		calls++
		if calls == 1 {
			panic("scorer dereferenced a nil provider")
		}
		return monitoring.EvalMeterResult{Score: 0.9, Baseline: 0.8}, nil
	})

	r.step(t)
	r.step(t)

	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2 (the run slot was not released)", calls)
	}
	if st := m.Status(); st.State != "ok" {
		t.Fatalf("state = %q, want ok once a later run succeeds", st.State)
	}
}

func TestMeter_CancelledRunKeepsLastGoodResult(t *testing.T) {
	calls := 0
	m, r := steppedMeter(t, func(ctx context.Context) (monitoring.EvalMeterResult, error) {
		calls++
		if calls == 1 {
			return monitoring.EvalMeterResult{Score: 0.81, Baseline: 0.80}, nil
		}
		return monitoring.EvalMeterResult{}, ctx.Err()
	})

	r.step(t)
	r.start(t, true)

	st := m.Status()
	if calls != 2 || st.State != "ok" || st.Error != "" || st.Score == nil || *st.Score != 0.81 {
		t.Fatalf("calls = %d, status = %+v, want ok with the last good score after a cancelled run", calls, st)
	}
}

func TestNewEvalMeter_RefusesAMissingTicker(t *testing.T) {
	_, err := monitoring.NewEvalMeter(0, clock.Now, nil, returning(monitoring.EvalMeterResult{}, nil), background.EveryPassLeads)

	if !errors.Is(err, monitoring.ErrEvalMeterTickerRequired) {
		t.Fatalf("err = %v, want ErrEvalMeterTickerRequired", err)
	}
}

func TestNewEvalMeter_RefusesAMissingRunnerOrLeadershipScope(t *testing.T) {
	runner := returning(monitoring.EvalMeterResult{}, nil)

	if _, err := monitoring.NewEvalMeter(0, nil, clock.NewTicker, runner, background.EveryPassLeads); !errors.Is(err, monitoring.ErrEvalMeterClockRequired) {
		t.Errorf("nil clock: err = %v, want ErrEvalMeterClockRequired", err)
	}
	if _, err := monitoring.NewEvalMeter(0, clock.Now, clock.NewTicker, nil, background.EveryPassLeads); !errors.Is(err, monitoring.ErrEvalMeterRunnerRequired) {
		t.Errorf("nil runner: err = %v, want ErrEvalMeterRunnerRequired", err)
	}
	if _, err := monitoring.NewEvalMeter(0, clock.Now, clock.NewTicker, runner, nil); !errors.Is(err, monitoring.ErrEvalMeterLeadershipRequired) {
		t.Errorf("nil scope: err = %v, want ErrEvalMeterLeadershipRequired", err)
	}
}
