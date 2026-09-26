package alert

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"
)

type Severity int

const (
	SeverityFix Severity = iota
	SeverityLog
	SeveritySignal
)

type Alert struct {
	Title    string
	Message  string
	Severity Severity
}

type AlertNotifier interface {
	Notify(ctx context.Context, a Alert) error
}

type Condition struct {
	Key  string
	Eval func(ctx context.Context) *Alert
}

// defaultEvalTimeout bounds a single condition evaluation so one stuck
// condition cannot freeze the monitor ticker forever.
const defaultEvalTimeout = 10 * time.Second

// defaultInterval stands in for a non-positive interval, which time.NewTicker
// rejects with a panic on the loop goroutine, where nothing can catch it.
const defaultInterval = 30 * time.Second

type LeadershipScope = runloop.LeadershipScope

// Monitor evaluates its conditions on a ticker and pages on each transition
// into an incident. Its exported surface is safe for concurrent use: Resume and
// the embedded kill switch publish through atomics. Its interior is not, and
// rests on Start spawning exactly one loop goroutine — see firing.
type Monitor struct {
	notifier    AlertNotifier
	conditions  []Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger
	leadership  LeadershipScope

	// resumes is bumped by any goroutine calling Resume; seenResumes is read
	// and written only by the loop goroutine, which also solely owns firing.
	resumes     atomic.Uint64
	seenResumes uint64

	// firing holds the keys currently in an incident. It carries no lock, and
	// is safe only because the loop goroutine owns it alone: evaluate and
	// rearmAfterResume are its only readers and writers, and both run from that
	// one goroutine. Reaching it from anywhere else races it.
	firing map[string]bool

	lastPass       atomic.Int64
	notifyFailures atomic.Int64
	notifyOKAt     atomic.Int64
	notifyErrorAt  atomic.Int64
	panics         atomic.Uint64
	runloop.Background
}

type Status struct {
	LastPass            time.Time
	LastNotifyOK        bool
	ConsecutiveFailures int64
	ContainedPanics     uint64
	NopNotifier         bool
	LastNotifyOKAt      time.Time
	LastNotifyErrorAt   time.Time
}

func (m *Monitor) Status() Status {
	failures := m.notifyFailures.Load()
	st := Status{
		LastNotifyOK:        failures == 0,
		ConsecutiveFailures: failures,
		ContainedPanics:     m.panics.Load(),
		NopNotifier:         m.notifierKind() == notifierKindNop,
	}
	if ns := m.lastPass.Load(); ns != 0 {
		st.LastPass = time.Unix(0, ns).UTC()
	}
	if ns := m.notifyOKAt.Load(); ns != 0 {
		st.LastNotifyOKAt = time.Unix(0, ns).UTC()
	}
	if ns := m.notifyErrorAt.Load(); ns != 0 {
		st.LastNotifyErrorAt = time.Unix(0, ns).UTC()
	}
	return st
}

const (
	notifierKindNop    = "nop"
	notifierKindCustom = "custom"
)

func (m *Monitor) notifierKind() string {
	switch m.notifier.(type) {
	case NopNotifier, *NopNotifier:
		return notifierKindNop
	default:
		return notifierKindCustom
	}
}

func NewMonitor(notifier AlertNotifier, interval time.Duration, conditions ...Condition) *Monitor {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Monitor{
		notifier:    notifier,
		conditions:  conditions,
		interval:    interval,
		evalTimeout: defaultEvalTimeout,
		logger:      slog.Default(),
		leadership:  runloop.EveryPassLeads,
		firing:      make(map[string]bool),
	}
}

// WithLeadership confines the monitor to the terms in which its instance leads.
// A deployment running more than one instance needs it: the loop is started
// once and outlives the term, so an instance whose lock was handed on would
// keep evaluating the same conditions as its successor and page each incident
// twice. A nil scope is ignored, leaving every pass leading.
func (m *Monitor) WithLeadership(scope LeadershipScope) *Monitor {
	if scope == nil {
		return m
	}
	m.leadership = scope
	return m
}

func (m *Monitor) Start(ctx context.Context) {
	m.Spawn(ctx, m.loop)
}

func (m *Monitor) loop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// tick honors the runtime kill switch and the leadership gate: a paused monitor
// skips evaluation entirely and resumes on the next tick after Resume, and an
// instance that is not currently leading skips it until its next term begins.
// The pass runs under the term's own context, so conditions still evaluating
// when the term ends are canceled instead of paging behind the new leader.
func (m *Monitor) tick(ctx context.Context) {
	m.rearmAfterResume()
	if m.Paused() {
		return
	}
	termCtx, release, ok := m.leadership(ctx)
	if !ok {
		return
	}
	defer release()
	m.evaluate(termCtx)
	m.lastPass.Store(time.Now().UnixNano())
}

// Resume clears the kill switch and marks the firing state stale, so the loop
// drops it before its next pass. Nothing is pushed while paused, so a key left
// marked firing would silence the incident that is open on Resume, including
// one that recovered and fired again inside the pause window.
func (m *Monitor) Resume() {
	m.resumes.Add(1)
	m.Background.Resume()
}

// rearmAfterResume discards the firing state once per Resume. It runs on the
// loop goroutine, which keeps the map single-owner: Resume only publishes a
// counter.
func (m *Monitor) rearmAfterResume() {
	resumes := m.resumes.Load()
	if resumes == m.seenResumes {
		return
	}
	m.seenResumes = resumes
	clear(m.firing)
}

// isPassAbandoned reports whether the rest of this pass must be dropped: an
// operator paused mid-pass, or the leadership term ended under it. A condition
// may take up to evalTimeout, so both are re-read between conditions and again
// before a push, not once per tick.
func (m *Monitor) isPassAbandoned(ctx context.Context) bool {
	return m.Paused() || ctx.Err() != nil
}

func (m *Monitor) evaluate(ctx context.Context) {
	for _, c := range m.conditions {
		if m.isPassAbandoned(ctx) {
			return
		}
		m.settleCondition(ctx, c)
	}
}

// settleCondition folds one condition's reading into the firing state, paging
// only on the transition into an incident.
func (m *Monitor) settleCondition(ctx context.Context, c Condition) {
	fired, known := m.runCondition(ctx, c)
	if !known {
		return
	}
	if fired == nil {
		m.recordRecovery(ctx, c.Key)
		return
	}
	if m.firing[c.Key] {
		return
	}
	m.raise(ctx, c.Key, *fired)
}

func (m *Monitor) recordRecovery(ctx context.Context, key string) {
	if !m.firing[key] {
		return
	}
	delete(m.firing, key)
	m.logger.InfoContext(ctx, "alert.recovered", "key", key)
}

func (m *Monitor) raise(ctx context.Context, key string, fired Alert) {
	if fired.Severity != SeveritySignal {
		m.logger.InfoContext(ctx, "alert.condition_firing", "key", key, "severity", int(fired.Severity))
		m.firing[key] = true
		return
	}
	if m.isPassAbandoned(ctx) {
		return
	}
	if err := m.notifier.Notify(ctx, fired); err != nil {
		// Do not mark firing: a failed push must re-arm so the next
		// tick retries instead of permanently silencing this key.
		m.notifyFailures.Add(1)
		m.notifyErrorAt.Store(time.Now().UnixNano())
		m.logger.ErrorContext(ctx, "alert.notify_failed", "key", key, "error", err)
		return
	}
	m.notifyFailures.Store(0)
	if m.notifierKind() != notifierKindNop {
		m.notifyOKAt.Store(time.Now().UnixNano())
	}
	m.logger.InfoContext(ctx, "alert.fired", "key", key, "severity", int(fired.Severity), "notifier", m.notifierKind())
	m.firing[key] = true
}

// runCondition evaluates one condition under a bounded timeout derived from the
// caller's context, so a blocking condition surfaces as a deadline rather than
// hanging the whole ticker. known is false when the condition panicked: its
// state is unreadable, so the caller must hold the last known one rather than
// take the panic for a recovery and page the same incident twice.
func (m *Monitor) runCondition(ctx context.Context, c Condition) (fired *Alert, known bool) {
	defer func() {
		if r := recover(); r != nil {
			m.panics.Add(1)
			fired, known = nil, false
			m.logger.ErrorContext(ctx, "alert.condition_panic",
				"key", c.Key, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	evalCtx, cancel := context.WithTimeout(ctx, m.evalTimeout)
	defer cancel()
	return c.Eval(evalCtx), true
}
