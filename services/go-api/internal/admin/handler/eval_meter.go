package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// evalDefaultInterval is deliberately long: a live eval run hits real provider
// APIs and competes with user traffic for per-provider quota.
const evalDefaultInterval = 6 * time.Hour

// EvalResult is the outcome of one discovery-eval run.
type EvalResult struct {
	Score     float64
	Baseline  float64
	Regressed bool
}

// EvalRunner performs one eval run. It MUST use a dedicated client that bypasses
// the shared per-provider circuit breakers (so eval failures never trip the
// breakers live search depends on) and a small fixed smoke query set.
type EvalRunner func(ctx context.Context) (EvalResult, error)

// EvalMeter periodically runs the discovery eval and exposes the latest score.
// It is inert unless explicitly enabled AND given a runner.
type EvalMeter struct {
	enabled  bool
	interval time.Duration
	runner   EvalRunner

	mu      sync.Mutex
	last    *EvalResult
	lastRun time.Time
	lastErr string
	running bool

	cancel context.CancelFunc
	done   chan struct{}
}

func NewEvalMeter(enabled bool, interval time.Duration, runner EvalRunner) *EvalMeter {
	if interval <= 0 {
		interval = evalDefaultInterval
	}
	return &EvalMeter{enabled: enabled, interval: interval, runner: runner}
}

// Start launches the eval ticker. It no-ops when disabled or when no runner is
// wired, so the meter is safe to construct unconditionally.
func (m *EvalMeter) Start(ctx context.Context) {
	if !m.enabled || m.runner == nil {
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	go m.loop(loopCtx)
}

func (m *EvalMeter) loop(ctx context.Context) {
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

func (m *EvalMeter) runOnce(ctx context.Context) {
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

// EvalStatus is the meter's wire shape for the console.
type EvalStatus struct {
	Enabled  bool       `json:"enabled"`
	State    string     `json:"state"` // disabled | no_data | ok | regression | error
	Score    *float64   `json:"score,omitempty"`
	Baseline *float64   `json:"baseline,omitempty"`
	LastRun  *time.Time `json:"last_run,omitempty"`
	Error    string     `json:"error,omitempty"`
}

func (m *EvalMeter) Status() EvalStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := EvalStatus{Enabled: m.enabled}
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
	}
	return st
}

// Shutdown stops the ticker, waiting up to the context deadline.
func (m *EvalMeter) Shutdown(ctx context.Context) {
	if m.cancel == nil {
		return
	}
	m.cancel()
	select {
	case <-m.done:
	case <-ctx.Done():
	}
}
