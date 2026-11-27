package searchqualitycheck

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	DefaultInterval   = 6 * time.Hour
	defaultRunTimeout = 5 * time.Minute
)

type QueryResult struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

type Result struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
	Queries   []QueryResult
}

type Runner func(ctx context.Context) (Result, error)

type Meter struct {
	now        clock.Func
	runTimeout time.Duration
	runner     Runner
	paused     func() bool

	mu      sync.Mutex
	last    *Result
	lastRun time.Time
	lastErr string
}

var (
	ErrClockRequired  = errors.New("the eval meter requires a clock")
	ErrRunnerRequired = errors.New("the eval meter requires a runner")
	ErrPausedRequired = errors.New("the eval meter requires a paused check")
)

func New(now clock.Func, runner Runner, paused func() bool) (*Meter, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	if runner == nil {
		return nil, ErrRunnerRequired
	}
	if paused == nil {
		return nil, ErrPausedRequired
	}
	return &Meter{
		now:        now,
		runTimeout: defaultRunTimeout,
		runner:     runner,
		paused:     paused,
	}, nil
}

func (m *Meter) Pass(ctx context.Context) error {
	res, err := m.runContained(ctx)
	m.recordRun(ctx, res, err)
	return err
}

func (m *Meter) runContained(ctx context.Context) (res Result, err error) {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	if panicked := background.SafeRun(ctx, "observe.eval_run_panicked", func() { res, err = m.runner(runCtx) }); panicked != nil {
		return Result{}, panicked
	}
	return res, err
}

func (m *Meter) recordRun(ctx context.Context, res Result, err error) {
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

type Status struct {
	Enabled  bool          `json:"enabled"`
	Paused   bool          `json:"paused"`
	State    State         `json:"state"`
	Score    *float64      `json:"score,omitempty"`
	Baseline *float64      `json:"baseline,omitempty"`
	Errored  int           `json:"errored,omitempty"`
	LastRun  *time.Time    `json:"last_run,omitempty"`
	Error    string        `json:"error,omitempty"`
	Queries  []QueryResult `json:"queries,omitempty"`
}

func (m *Meter) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := Status{Enabled: true, Paused: m.paused()}
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

func (m *Meter) fillLastResult(st *Status) {
	st.State = StateOK
	if m.last.Regressed {
		st.State = StateRegression
	}
	score, base, lr := m.last.Score, m.last.Baseline, m.lastRun
	st.Score, st.Baseline, st.LastRun = &score, &base, &lr
	st.Errored = m.last.Errored
	st.Queries = m.last.Queries
}
