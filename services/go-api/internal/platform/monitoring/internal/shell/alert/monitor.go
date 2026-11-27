package alert

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	defaultEvalTimeout = 10 * time.Second
	passGapTolerance   = 2
)

type Monitor struct {
	notifier    ports.AlertNotifier
	conditions  []domain.Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger
	now         clock.Func

	firing map[string]bool
	pages  map[string]domain.Alert

	lastPass       atomic.Int64
	notifyFailures atomic.Int64
	notifyOKAt     atomic.Int64
	notifyErrorAt  atomic.Int64
	panics         atomic.Uint64
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
	if m.notifier.Silent() {
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
		now:         now,
		firing:      make(map[string]bool),
		pages:       make(map[string]domain.Alert),
	}, nil
}

func (m *Monitor) Pass(ctx context.Context) error {
	m.forgetIncidentsAfterGap()
	m.evaluate(ctx)
	m.lastPass.Store(m.now().UnixNano())
	return nil
}

func (m *Monitor) forgetIncidentsAfterGap() {
	last := m.lastPass.Load()
	if last != 0 && m.now().Sub(time.Unix(0, last)) > passGapTolerance*m.interval {
		m.forgetIncidents()
	}
}

func (m *Monitor) forgetIncidents() {
	clear(m.firing)
	clear(m.pages)
}

func (m *Monitor) isPassAbandoned(ctx context.Context) bool {
	return ctx.Err() != nil
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
		m.logger.ErrorContext(ctx, "alert.notify_failed", "key", key, logging.KeyError, err)
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
