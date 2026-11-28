package service_test

import (
	"altune/go-api/internal/platform/monitoring/internal/service"
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

func never() bool { return false }

func mustMeter(t *testing.T, runner service.EvalMeterRunner) *service.EvalMeter {
	t.Helper()
	m, err := service.NewEvalMeter(clock.Now, runner, never)
	if err != nil {
		t.Fatalf("NewEvalMeter: %v", err)
	}
	return m
}

func returning(res service.EvalMeterResult, err error) service.EvalMeterRunner {
	return func(context.Context) (service.EvalMeterResult, error) { return res, err }
}

func wireState(t *testing.T, m *service.EvalMeter) string {
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

func meterAfterOneRun(t *testing.T, res service.EvalMeterResult, err error) *service.EvalMeter {
	t.Helper()
	m := mustMeter(t, returning(res, err))
	_ = m.Pass(t.Context())
	return m
}

func TestStatus_MarshalsEachStateAsItsWireString(t *testing.T) {
	meters := map[string]*service.EvalMeter{
		"no_data":    mustMeter(t, returning(service.EvalMeterResult{}, nil)),
		"ok":         meterAfterOneRun(t, service.EvalMeterResult{Score: 0.9, Baseline: 0.8}, nil),
		"regression": meterAfterOneRun(t, service.EvalMeterResult{Score: 0.7, Baseline: 0.8, Regressed: true}, nil),
		"error":      meterAfterOneRun(t, service.EvalMeterResult{}, errors.New("provider unreachable")),
	}

	for want, m := range meters {
		if got := wireState(t, m); got != want {
			t.Errorf("state = %q, want %q", got, want)
		}
	}
}

func TestMeter_IsAlwaysEnabled(t *testing.T) {
	m := mustMeter(t, returning(service.EvalMeterResult{}, nil))
	if st := m.Status(); !st.Enabled || st.State == "disabled" {
		t.Errorf("status = %+v, want enabled and never disabled", st)
	}
}

func TestMeter_NoDataBeforeFirstRun(t *testing.T) {
	m := mustMeter(t, returning(service.EvalMeterResult{}, nil))
	if st := m.Status(); st.State != "no_data" {
		t.Fatalf("state = %q, want no_data before any run", st.State)
	}
}

func TestMeter_OkAndRegression(t *testing.T) {
	m := meterAfterOneRun(t, service.EvalMeterResult{Score: 0.81, Baseline: 0.80}, nil)
	st := m.Status()
	if st.State != "ok" || st.Score == nil || *st.Score != 0.81 || st.Baseline == nil || *st.Baseline != 0.80 {
		t.Fatalf("status = %+v, want ok with score 0.81 and baseline 0.80", st)
	}
	if st.LastRun == nil {
		t.Errorf("status = %+v, want the last run time", st)
	}

	m2 := meterAfterOneRun(t, service.EvalMeterResult{Score: 0.70, Baseline: 0.80, Regressed: true}, nil)
	if st := m2.Status(); st.State != "regression" {
		t.Fatalf("state = %q, want regression", st.State)
	}
}

func TestMeter_StatusCarriesTheQueries(t *testing.T) {
	queries := []service.EvalMeterQueryResult{{Query: "q", Expect: "e", Passed: true, Position: 1}}
	m := meterAfterOneRun(t, service.EvalMeterResult{Score: 0.9, Baseline: 0.8, Queries: queries}, nil)

	if got := m.Status().Queries; len(got) != 1 || got[0] != queries[0] {
		t.Errorf("queries = %+v, want %+v", got, queries)
	}
}

func TestMeter_ErrorState(t *testing.T) {
	m := meterAfterOneRun(t, service.EvalMeterResult{}, errors.New("provider unreachable"))
	if st := m.Status(); st.State != "error" || st.Error != "provider unreachable" || st.LastRun == nil {
		t.Fatalf("status = %+v, want error state with the message and run time", st)
	}
}

func TestMeter_StatusSurfacesErroredQueryCount(t *testing.T) {
	m := meterAfterOneRun(t, service.EvalMeterResult{Score: 0.60, Baseline: 0.80, Errored: 2}, nil)

	st := m.Status()
	if st.Errored != 2 {
		t.Errorf("Errored = %d, want 2", st.Errored)
	}
	if st.State == "regression" {
		t.Error("a sub-baseline score with errored queries must not read as a regression")
	}
}

func TestMeter_PassReturnsTheRunnersErrorForTheJobHealth(t *testing.T) {
	boom := errors.New("provider unreachable")
	m := mustMeter(t, returning(service.EvalMeterResult{}, boom))

	if err := m.Pass(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("Pass error = %v, want the runner's error", err)
	}
	if err := mustMeter(t, returning(service.EvalMeterResult{Score: 1}, nil)).Pass(t.Context()); err != nil {
		t.Fatalf("Pass error = %v, want nil after a good run", err)
	}
}

func TestMeter_RunnerRunsUnderAFiveMinuteDeadline(t *testing.T) {
	var remaining time.Duration
	var hasDeadline bool
	m := mustMeter(t, func(ctx context.Context) (service.EvalMeterResult, error) {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		remaining = time.Until(deadline)
		return service.EvalMeterResult{}, context.DeadlineExceeded
	})

	_ = m.Pass(t.Context())

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

func TestMeter_StatusReportsPausedFromTheInjectedCheck(t *testing.T) {
	paused := false
	m, err := service.NewEvalMeter(clock.Now, returning(service.EvalMeterResult{}, nil), func() bool { return paused })
	if err != nil {
		t.Fatalf("NewEvalMeter: %v", err)
	}

	if m.Status().Paused {
		t.Fatal("status paused before the check says so")
	}
	paused = true
	if !m.Status().Paused {
		t.Fatal("status not paused although the check says so")
	}
}

func TestMeter_PanickingRunnerSurfacesAsError(t *testing.T) {
	m := mustMeter(t, func(context.Context) (service.EvalMeterResult, error) {
		panic("scorer dereferenced a nil provider")
	})

	_ = m.Pass(t.Context())

	st := m.Status()
	if st.State != "error" {
		t.Fatalf("state = %q, want error after a panicking runner", st.State)
	}
	if !strings.Contains(st.Error, "scorer dereferenced a nil provider") {
		t.Errorf("error = %q, want the panic value in it", st.Error)
	}
}

func TestMeter_APanickingRunIsLoggedOnceByTheGuard(t *testing.T) {
	t.Cleanup(logging.Swap(slog.Default()))
	ring := logging.Setup("error")
	m := mustMeter(t, func(context.Context) (service.EvalMeterResult, error) {
		panic("scorer dereferenced a nil provider")
	})

	_ = m.Pass(t.Context())

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
	t.Cleanup(logging.Swap(slog.Default()))
	ring := logging.Setup("error")
	m := mustMeter(t, returning(service.EvalMeterResult{}, errors.New("golden set unreadable")))

	_ = m.Pass(t.Context())

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
	m := mustMeter(t, func(context.Context) (service.EvalMeterResult, error) {
		calls++
		if calls == 1 {
			panic("scorer dereferenced a nil provider")
		}
		return service.EvalMeterResult{Score: 0.9, Baseline: 0.8}, nil
	})

	_ = m.Pass(t.Context())
	_ = m.Pass(t.Context())

	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2", calls)
	}
	if st := m.Status(); st.State != "ok" {
		t.Fatalf("state = %q, want ok once a later run succeeds", st.State)
	}
}

func TestMeter_CancelledRunKeepsLastGoodResult(t *testing.T) {
	calls := 0
	m := mustMeter(t, func(ctx context.Context) (service.EvalMeterResult, error) {
		calls++
		if calls == 1 {
			return service.EvalMeterResult{Score: 0.81, Baseline: 0.80}, nil
		}
		return service.EvalMeterResult{}, ctx.Err()
	})

	_ = m.Pass(t.Context())
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_ = m.Pass(cancelled)

	st := m.Status()
	if calls != 2 || st.State != "ok" || st.Error != "" || st.Score == nil || *st.Score != 0.81 {
		t.Fatalf("calls = %d, status = %+v, want ok with the last good score after a cancelled run", calls, st)
	}
}

func TestNewEvalMeter_RefusesAMissingCollaborator(t *testing.T) {
	runner := returning(service.EvalMeterResult{}, nil)

	if _, err := service.NewEvalMeter(nil, runner, never); !errors.Is(err, service.ErrEvalMeterClockRequired) {
		t.Errorf("nil clock: err = %v, want ErrEvalMeterClockRequired", err)
	}
	if _, err := service.NewEvalMeter(clock.Now, nil, never); !errors.Is(err, service.ErrEvalMeterRunnerRequired) {
		t.Errorf("nil runner: err = %v, want ErrEvalMeterRunnerRequired", err)
	}
	if _, err := service.NewEvalMeter(clock.Now, runner, nil); !errors.Is(err, service.ErrEvalMeterPausedRequired) {
		t.Errorf("nil paused check: err = %v, want ErrEvalMeterPausedRequired", err)
	}
}
