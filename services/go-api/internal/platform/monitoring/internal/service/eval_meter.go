package service

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	EvalMeterInterval = 6 * time.Hour
	defaultRunTimeout = 5 * time.Minute
)

type EvalMeterQueryResult struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

type EvalMeterResult struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
	Queries   []EvalMeterQueryResult
}

type EvalMeterRunner func(ctx context.Context) (EvalMeterResult, error)

type EvalMeter struct {
	now        clock.Func
	runTimeout time.Duration
	runner     EvalMeterRunner
	paused     func() bool

	mu      sync.Mutex
	last    *EvalMeterResult
	lastRun time.Time
	lastErr string
}

var (
	ErrEvalMeterClockRequired  = errors.New("the eval meter requires a clock")
	ErrEvalMeterRunnerRequired = errors.New("the eval meter requires a runner")
	ErrEvalMeterPausedRequired = errors.New("the eval meter requires a paused check")
)

func NewEvalMeter(now clock.Func, runner EvalMeterRunner, paused func() bool) (*EvalMeter, error) {
	if now == nil {
		return nil, ErrEvalMeterClockRequired
	}
	if runner == nil {
		return nil, ErrEvalMeterRunnerRequired
	}
	if paused == nil {
		return nil, ErrEvalMeterPausedRequired
	}
	return &EvalMeter{
		now:        now,
		runTimeout: defaultRunTimeout,
		runner:     runner,
		paused:     paused,
	}, nil
}

func (m *EvalMeter) Pass(ctx context.Context) error {
	res, err := m.runContained(ctx)
	m.recordRun(ctx, res, err)
	return err
}

func (m *EvalMeter) runContained(ctx context.Context) (res EvalMeterResult, err error) {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	if panicked := background.SafeRun(ctx, "observe.eval_run_panicked", func() { res, err = m.runner(runCtx) }); panicked != nil {
		return EvalMeterResult{}, fmt.Errorf("run background task: %w", panicked)
	}
	return res, err
}

func (m *EvalMeter) recordRun(ctx context.Context, res EvalMeterResult, err error) {
	if err != nil && ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRun = m.now.UTC()
	if err != nil {
		m.lastErr = err.Error()
		logRunFailure(ctx, err)
		return
	}
	m.last = &res
	m.lastErr = ""
}

func logRunFailure(ctx context.Context, err error) {
	var panicked *background.PanicError
	if errors.As(err, &panicked) {
		return
	}
	slog.ErrorContext(ctx, "observe.eval_run_failed", logging.KeyError, err)
}

type State string

const (
	StateDisabled   State = "disabled"
	StateNoData     State = "no_data"
	StateOK         State = "ok"
	StateRegression State = "regression"
	StateError      State = "error"
)

type MeterStatus struct {
	Enabled  bool                   `json:"enabled"`
	Paused   bool                   `json:"paused"`
	State    State                  `json:"state"`
	Score    *float64               `json:"score,omitempty"`
	Baseline *float64               `json:"baseline,omitempty"`
	Errored  int                    `json:"errored,omitempty"`
	LastRun  *time.Time             `json:"last_run,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Queries  []EvalMeterQueryResult `json:"queries,omitempty"`
}

func (m *EvalMeter) Status() MeterStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := MeterStatus{Enabled: true, Paused: m.paused()}
	switch {
	case m.lastErr != "":
		st.State = StateError
		st.Error = m.lastErr
		if !m.lastRun.IsZero() {
			lr := m.lastRun
			st.LastRun = &lr
		}
	case m.last == nil:
		st.State = StateNoData
	default:
		m.fillLastResult(&st)
	}
	return st
}

func (m *EvalMeter) fillLastResult(st *MeterStatus) {
	st.State = StateOK
	if m.last.Regressed {
		st.State = StateRegression
	}
	score, base, lr := m.last.Score, m.last.Baseline, m.lastRun
	st.Score, st.Baseline, st.LastRun = &score, &base, &lr
	st.Errored = m.last.Errored
	st.Queries = m.last.Queries
}
