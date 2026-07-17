// Package evalmeter runs the Mission Control discovery-eval meter: a background
// ticker that periodically scores the live search pipeline against a small fixed
// smoke-query set and exposes the latest score to the console. It is a lifecycle
// component, not a handler — the admin handler only reads Status().
package evalmeter

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// defaultInterval is deliberately long: a live eval run hits real provider
// APIs and competes with user traffic for per-provider quota.
const defaultInterval = 6 * time.Hour

// QueryResult is one smoke query's outcome in an eval run: whether the
// expected result landed in the top-K, and at what position (-1 if absent).
type QueryResult struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

// Result is the outcome of one discovery-eval run.
type Result struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Queries   []QueryResult
}

// Runner performs one eval run. It MUST use a dedicated client that bypasses
// the shared per-provider circuit breakers (so eval failures never trip the
// breakers live search depends on) and a small fixed smoke query set.
type Runner func(ctx context.Context) (Result, error)

// Meter periodically runs the discovery eval and exposes the latest score.
// It is inert unless explicitly enabled AND given a runner.
type Meter struct {
	enabled  bool
	interval time.Duration
	runner   Runner

	mu      sync.Mutex
	last    *Result
	lastRun time.Time
	lastErr string
	running bool

	cancel context.CancelFunc
	done   chan struct{}
}

func New(enabled bool, interval time.Duration, runner Runner) *Meter {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Meter{enabled: enabled, interval: interval, runner: runner}
}

// Start launches the eval ticker. It no-ops when disabled or when no runner is
// wired, so the meter is safe to construct unconditionally.
func (m *Meter) Start(ctx context.Context) {
	if !m.enabled || m.runner == nil {
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	go m.loop(loopCtx)
}

func (m *Meter) loop(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.runOnce(ctx) // one run on start so the meter isn't empty for a full interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
		}
	}
}

func (m *Meter) runOnce(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return // skip-if-running: never overlap eval runs
	}
	m.running = true
	m.mu.Unlock()

	res, err := m.runner(ctx)

	m.mu.Lock()
	m.running = false
	m.lastRun = time.Now().UTC()
	if err != nil {
		m.lastErr = err.Error()
		slog.ErrorContext(ctx, "admin.eval_run_failed", "error", err)
	} else {
		r := res
		m.last = &r
		m.lastErr = ""
	}
	m.mu.Unlock()
}

// Status is the meter's wire shape for the console.
type Status struct {
	Enabled  bool          `json:"enabled"`
	State    string        `json:"state"` // disabled | no_data | ok | regression | error
	Score    *float64      `json:"score,omitempty"`
	Baseline *float64      `json:"baseline,omitempty"`
	LastRun  *time.Time    `json:"last_run,omitempty"`
	Error    string        `json:"error,omitempty"`
	Queries  []QueryResult `json:"queries,omitempty"`
}

func (m *Meter) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := Status{Enabled: m.enabled}
	switch {
	case !m.enabled:
		st.State = "disabled"
	case m.lastErr != "":
		st.State = "error"
		st.Error = m.lastErr
		if !m.lastRun.IsZero() {
			lr := m.lastRun
			st.LastRun = &lr
		}
	case m.last == nil:
		st.State = "no_data"
	default:
		st.State = "ok"
		if m.last.Regressed {
			st.State = "regression"
		}
		score, base, lr := m.last.Score, m.last.Baseline, m.lastRun
		st.Score, st.Baseline, st.LastRun = &score, &base, &lr
		st.Queries = m.last.Queries
	}
	return st
}

// Shutdown stops the ticker, waiting up to the context deadline.
func (m *Meter) Shutdown(ctx context.Context) {
	if m.cancel == nil {
		return
	}
	m.cancel()
	select {
	case <-m.done:
	case <-ctx.Done():
	}
}
