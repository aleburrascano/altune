package evalmeter

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultInterval   = 6 * time.Hour
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

type LeadershipScope = runloop.LeadershipScope

type Meter struct {
	interval   time.Duration
	runTimeout time.Duration
	runner     Runner
	leadership LeadershipScope

	mu      sync.Mutex
	last    *Result
	lastRun time.Time
	lastErr string

	runloop.Background
}

func New(interval time.Duration, runner Runner) *Meter {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Meter{
		interval:   interval,
		runTimeout: defaultRunTimeout,
		runner:     runner,
		leadership: runloop.EveryPassLeads,
	}
}

func (m *Meter) WithLeadership(scope LeadershipScope) *Meter {
	if scope == nil {
		return m
	}
	m.leadership = scope
	return m
}

func (m *Meter) Start(ctx context.Context) {
	if m.runner == nil {
		return
	}
	m.Spawn(ctx, m.loop)
}

func (m *Meter) loop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *Meter) tick(ctx context.Context) {
	if m.Paused() {
		return
	}
	termCtx, release, ok := m.leadership(ctx)
	if !ok {
		return
	}
	defer release()
	m.runOnce(termCtx)
}

func (m *Meter) runOnce(ctx context.Context) {
	res, err := m.runContained(ctx)
	m.recordRun(ctx, res, err)
}

func (m *Meter) runContained(ctx context.Context) (res Result, err error) {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	if panicked := runloop.Try(ctx, "observe.eval_run_panicked", func() { res, err = m.runner(runCtx) }); panicked != nil {
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
	m.lastRun = time.Now().UTC()
	if err != nil {
		m.lastErr = err.Error()
		logRunFailure(ctx, err)
		return
	}
	m.last = &res
	m.lastErr = ""
}

func logRunFailure(ctx context.Context, err error) {
	var panicked *runloop.PanicError
	if errors.As(err, &panicked) {
		return
	}
	slog.ErrorContext(ctx, "observe.eval_run_failed", "error", err)
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

	st := Status{Enabled: true, Paused: m.Paused()}
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
