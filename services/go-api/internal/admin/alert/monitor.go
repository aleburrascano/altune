package alert

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/shared/runloop"
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

type Monitor struct {
	notifier    AlertNotifier
	conditions  []Condition
	interval    time.Duration
	evalTimeout time.Duration
	logger      *slog.Logger

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
		firing:      make(map[string]bool),
	}
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

// tick honors the runtime kill switch: a paused monitor skips evaluation
// entirely and resumes on the next tick after Resume.
func (m *Monitor) tick(ctx context.Context) {
	if m.Paused() {
		return
	}
	m.evaluate(ctx)
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
