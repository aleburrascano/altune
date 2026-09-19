package evalmeter

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"sync"
	"time"
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

// LeadershipScope scopes one run to the caller's current leadership term: ok is
// false when this instance must not run the eval at all, and the context it
// returns is canceled the moment the term ends, so a run already in flight is
// cut off rather than outliving the term. release ends the run.
type LeadershipScope func(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)

// everyRunLeads is the default scope: without an election behind it this
// process is the only one metering, so every run proceeds, under a plain child
// of the caller's context.
func everyRunLeads(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}

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
		leadership: everyRunLeads,
	}
}

// WithLeadership confines the meter to the terms in which its instance leads.
// A deployment running more than one instance needs it: the loop is started
// once and outlives the term, so an instance whose lock was handed on would
// keep paying for the same eval its successor is already running. A nil scope
// is ignored, leaving every run leading.
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

// tick honors the runtime kill switch and the leadership gate: a paused meter
// skips its scheduled run and runs again on the next tick after Resume, and an
// instance that is not currently leading skips it until its next term begins.
// The run takes the term's own context, so an eval still in flight when the
// term ends is canceled instead of competing with the new leader's.
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
	Paused   bool          `json:"paused"`
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
		st.Queries = m.last.Queries
	}
	return st
}
