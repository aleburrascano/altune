package evalmeter

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
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

// Errored is how many of Queries failed to run at all. Such a query is scored
// as a failed check, so a nonzero count means Score is a floor rather than a
// ranking verdict, and the runner leaves Regressed false for that run.
type Result struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
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

// Meter schedules the eval run and holds its latest verdict. Once started it is
// safe for concurrent use: mu guards the verdict and the run slot, so the loop
// goroutine records a run while operator requests read Status, and the kill
// switch is atomic. The configuration above mu is not: WithLeadership and every
// New argument must be settled before Start.
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
	defer m.releaseRunSlot()
	res, err := m.runContained(ctx)
	m.recordRun(ctx, res, err)
}

// runContained invokes the runner under the run timeout and turns a panic into
// an ordinary failed run: the meter ticks on a background goroutine, where an
// escaping panic terminates the whole process.
func (m *Meter) runContained(ctx context.Context) (res Result, err error) {
	runCtx, cancel := context.WithTimeout(ctx, m.runTimeout)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "admin.eval_run_panicked",
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
		slog.ErrorContext(ctx, "admin.eval_run_failed", "error", err)
		return
	}
	m.last = &res
	m.lastErr = ""
}

// State is the status vocabulary of a meter. Its values are the wire form the
// admin client branches on, so they are fixed even as the type keeps a caller
// from inventing one.
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

// releaseRunSlot is deferred by its claimer: a run that ends without releasing
// leaves the meter idle-but-claimed, and every later run is skipped for the
// lifetime of the process.
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
