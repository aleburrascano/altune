package evalmeter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/shared/runloop"
)

const defaultInterval = 6 * time.Hour

// defaultRunTimeout bounds a single runner invocation so a hanging runner
// cannot permanently stall the scheduler (claimRunSlotIfIdle stays true).
const defaultRunTimeout = 5 * time.Minute

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
	Queries   []QueryResult
}

type Runner func(ctx context.Context) (Result, error)

type Meter struct {
	enabled    bool
	interval   time.Duration
	runTimeout time.Duration
	runner     Runner

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
	return &Meter{enabled: enabled, interval: interval, runTimeout: defaultRunTimeout, runner: runner}
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

// tick honors the runtime kill switch: a paused meter skips its scheduled run
// and runs again on the next tick after Resume.
func (m *Meter) tick(ctx context.Context) {
	if m.Paused() {
		return
	}
	m.runOnce(ctx)
}

func (m *Meter) runOnce(ctx context.Context) {
	if !m.claimRunSlotIfIdle() {
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	res, err := m.runner(runCtx)
	cancel()

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

const (
	StateDisabled   = "disabled"
	StateNoData     = "no_data"
	StateOK         = "ok"
	StateRegression = "regression"
	StateError      = "error"
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

type Status struct {
	Enabled  bool          `json:"enabled"`
	State    string        `json:"state"`
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
		st.Queries = m.last.Queries
	}
	return st
}
