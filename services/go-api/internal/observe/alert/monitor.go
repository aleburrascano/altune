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

const defaultEvalTimeout = 10 * time.Second

const defaultInterval = 30 * time.Second

type LeadershipScope = runloop.LeadershipScope

type Monitor struct {
	notifier    AlertNotifier
	conditions  []Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger
	leadership  LeadershipScope

	resumes     atomic.Uint64
	seenResumes uint64

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

func (m *Monitor) Resume() {
	m.resumes.Add(1)
	m.Background.Resume()
}

func (m *Monitor) rearmAfterResume() {
	resumes := m.resumes.Load()
	if resumes == m.seenResumes {
		return
	}
	m.seenResumes = resumes
	clear(m.firing)
}

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
