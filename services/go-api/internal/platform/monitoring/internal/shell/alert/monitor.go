package alert

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

const defaultEvalTimeout = 10 * time.Second

type LeadershipScope = background.OnlyWhileLeader

type Monitor struct {
	notifier    ports.AlertNotifier
	conditions  []domain.Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger
	leadership  LeadershipScope
	now         clock.Func

	resumes     atomic.Uint64
	seenResumes uint64

	firing map[string]bool
	pages  map[string]domain.Alert

	lastPass       atomic.Int64
	notifyFailures atomic.Int64
	notifyOKAt     atomic.Int64
	notifyErrorAt  atomic.Int64
	panics         atomic.Uint64
	background.Background
}

type Status struct {
	LastPass            time.Time `json:"last_pass"`
	LastNotifyOK        bool      `json:"last_notify_ok"`
	ConsecutiveFailures int64     `json:"consecutive_failures"`
	ContainedPanics     uint64    `json:"contained_panics"`
	NopNotifier         bool      `json:"nop_notifier"`
	LastNotifyOKAt      time.Time `json:"last_notify_ok_at"`
	LastNotifyErrorAt   time.Time `json:"last_notify_error_at"`
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
	if s, ok := m.notifier.(ports.SilentNotifier); ok && s.Silent() {
		return notifierKindNop
	}
	return notifierKindCustom
}

var ErrInvalidInterval = errors.New("the interval must be positive")

var ErrClockRequired = errors.New("the alert monitor requires a clock")

func NewMonitor(notifier ports.AlertNotifier, interval time.Duration, now clock.Func, conditions ...domain.Condition) (*Monitor, error) {
	if interval <= 0 {
		return nil, ErrInvalidInterval
	}
	if now == nil {
		return nil, ErrClockRequired
	}
	return &Monitor{
		notifier:    notifier,
		conditions:  conditions,
		interval:    interval,
		evalTimeout: defaultEvalTimeout,
		logger:      slog.Default(),
		leadership:  background.EveryPassLeads,
		now:         now,
		firing:      make(map[string]bool),
		pages:       make(map[string]domain.Alert),
	}, nil
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
		m.forgetIncidents()
		return
	}
	defer release()
	m.evaluate(termCtx)
	m.lastPass.Store(m.now().UnixNano())
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
	m.forgetIncidents()
}

func (m *Monitor) forgetIncidents() {
	clear(m.firing)
	clear(m.pages)
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

func (m *Monitor) settleCondition(ctx context.Context, c domain.Condition) {
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
	delete(m.pages, key)
	if !m.firing[key] {
		return
	}
	delete(m.firing, key)
	m.logger.InfoContext(ctx, "alert.recovered", "key", key)
}

func (m *Monitor) raise(ctx context.Context, key string, fired domain.Alert) {
	if fired.Severity != domain.SeverityWatch {
		m.logger.InfoContext(ctx, "alert.condition_firing", "key", key, "severity", int(fired.Severity))
		m.firing[key] = true
		return
	}
	if m.isPassAbandoned(ctx) {
		return
	}
	if err := m.notifier.Notify(ctx, m.pageFor(key, fired)); err != nil {
		m.notifyFailures.Add(1)
		m.notifyErrorAt.Store(m.now().UnixNano())
		m.logger.ErrorContext(ctx, "alert.notify_failed", "key", key, "error", err)
		return
	}
	m.notifyFailures.Store(0)
	if m.notifierKind() != notifierKindNop {
		m.notifyOKAt.Store(m.now().UnixNano())
	}
	m.logger.InfoContext(ctx, "alert.fired", "key", key, "severity", int(fired.Severity), "notifier", m.notifierKind())
	m.firing[key] = true
}

func (m *Monitor) pageFor(key string, fired domain.Alert) domain.Alert {
	if page, ok := m.pages[key]; ok {
		return page
	}
	fired.Key, fired.FiredAt = key, m.now()
	m.pages[key] = fired
	return fired
}

func (m *Monitor) runCondition(ctx context.Context, c domain.Condition) (fired *domain.Alert, known bool) {
	evalCtx, cancel := context.WithTimeout(ctx, m.evalTimeout)
	defer cancel()
	if panicked := background.SafeRun(ctx, "alert.condition_panic", func() { fired = c.Eval(evalCtx) }, "key", c.Key); panicked != nil {
		m.panics.Add(1)
		return nil, false
	}
	return fired, true
}
