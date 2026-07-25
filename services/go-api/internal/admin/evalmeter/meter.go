package evalmeter

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const defaultInterval = 6 * time.Hour

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
	m.runOnce(ctx)
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
	if !m.claimRunSlotIfIdle() {
		return
	}

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
