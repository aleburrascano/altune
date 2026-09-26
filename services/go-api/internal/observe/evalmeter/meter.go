package evalmeter

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"altune/go-api/internal/shared/runloop"
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
	enabled    bool
	interval   time.Duration
	runTimeout time.Duration
	runner     Runner
	leadership LeadershipScope

	mu      sync.Mutex
	last    *Result
	lastRun time.Time
	lastErr string
	running bool

	runloop.Background
}

func New(enabled bool, interval time.Duration, runner Runner) *Meter {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Meter{
		enabled:    enabled,
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
	if !m.enabled || m.runner == nil {
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
	if !m.claimRunSlotIfIdle() {
		return
	}
	defer m.releaseRunSlot()
	res, err := m.runContained(ctx)
	m.recordRun(ctx, res, err)
}

func (m *Meter) runContained(ctx context.Context) (res Result, err error) {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "observe.eval_run_panicked",
				"panic", rec, "stack", string(debug.Stack()))
			res, err = Result{}, fmt.Errorf("panic: %v", rec)
		}
	}()
	return m.runner(runCtx)
}

func (m *Meter) recordRun(ctx context.Context, res Result, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRun = time.Now().UTC()
	if err != nil {
		m.lastErr = err.Error()
		slog.ErrorContext(ctx, "observe.eval_run_failed", "error", err)
		return
	}
	m.last = &res
	m.lastErr = ""
}

type State string

const (
	StateDisabled   State = "disabled"
	StateNoData     State = "no_data"
	StateOK         State = "ok"
	StateRegression State = "regression"
	StateError      State = "error"
)

func (m *Meter) claimRunSlotIfIdle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *Meter) releaseRunSlot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
}

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

	st := Status{Enabled: m.enabled, Paused: m.Paused()}
	switch {
	case !m.enabled:
		st.State = StateDisabled
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
		st.State = StateOK
		if m.last.Regressed {
			st.State = StateRegression
		}
		score, base, lr := m.last.Score, m.last.Baseline, m.lastRun
		st.Score, st.Baseline, st.LastRun = &score, &base, &lr
		st.Errored = m.last.Errored
		st.Queries = m.last.Queries
	}
	return st
}
