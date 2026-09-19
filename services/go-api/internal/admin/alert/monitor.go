package alert

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
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

// LeadershipScope scopes one pass to the caller's current leadership term: ok
// is false when this instance must not evaluate at all, and the context it
// returns is canceled the moment the term ends, so an evaluation already in
// flight is cut off rather than outliving the term. release ends the pass.
type LeadershipScope func(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)

// everyPassLeads is the default scope: without an election behind it this
// process is the only one monitoring, so every pass proceeds, under a plain
// child of the caller's context.
func everyPassLeads(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}

type Monitor struct {
	notifier    AlertNotifier
	conditions  []Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger
	leadership  LeadershipScope

	firing map[string]bool
	runloop.Background
}

func NewMonitor(notifier AlertNotifier, interval time.Duration, conditions ...Condition) *Monitor {
	return &Monitor{
		notifier:    notifier,
		conditions:  conditions,
		interval:    interval,
		evalTimeout: defaultEvalTimeout,
		logger:      slog.Default(),
		leadership:  everyPassLeads,
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
	if m.Paused() {
		return
	}
	termCtx, release, ok := m.leadership(ctx)
	if !ok {
		return
	}
	defer release()
	m.evaluate(termCtx)
}

func (m *Monitor) evaluate(ctx context.Context) {
	for _, c := range m.conditions {
		fired := m.runCondition(ctx, c)
		wasFiring := m.firing[c.Key]

		if fired == nil {
			if wasFiring {
				delete(m.firing, c.Key)
				m.logger.InfoContext(ctx, "alert.recovered", "key", c.Key)
			}
			continue
		}

		if wasFiring {
			continue
		}

		if fired.Severity != SeveritySignal {
			m.logger.InfoContext(ctx, "alert.condition_firing", "key", c.Key, "severity", int(fired.Severity))
			m.firing[c.Key] = true
			continue
		}
		if err := m.notifier.Notify(ctx, *fired); err != nil {
			// Do not mark firing: a failed push must re-arm so the next
			// tick retries instead of permanently silencing this key.
			m.logger.ErrorContext(ctx, "alert.notify_failed", "key", c.Key, "error", err)
			continue
		}
		m.firing[c.Key] = true
	}
}

// runCondition evaluates one condition under a bounded timeout derived from the
// caller's context, so a blocking condition surfaces as a deadline rather than
// hanging the whole ticker.
func (m *Monitor) runCondition(ctx context.Context, c Condition) *Alert {
	evalCtx, cancel := context.WithTimeout(ctx, m.evalTimeout)
	defer cancel()
	return c.Eval(evalCtx)
}
