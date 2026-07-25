package alert

import (
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

type Monitor struct {
	notifier   AlertNotifier
	conditions []Condition
	interval   time.Duration
	logger     *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
	firing map[string]bool
}

func NewMonitor(notifier AlertNotifier, interval time.Duration, conditions ...Condition) *Monitor {
	return &Monitor{
		notifier:   notifier,
		conditions: conditions,
		interval:   interval,
		logger:     slog.Default(),
		firing:     make(map[string]bool),
	}
}

func (m *Monitor) Start(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})
	go m.loop(loopCtx)
}

func (m *Monitor) loop(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.evaluate(ctx)
		}
	}
}

func (m *Monitor) evaluate(ctx context.Context) {
	for _, c := range m.conditions {
		fired := c.Eval(ctx)
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
		m.firing[c.Key] = true

		if fired.Severity != SeveritySignal {
			m.logger.InfoContext(ctx, "alert.condition_firing", "key", c.Key, "severity", int(fired.Severity))
			continue
		}
		if err := m.notifier.Notify(ctx, *fired); err != nil {
			m.logger.ErrorContext(ctx, "alert.notify_failed", "key", c.Key, "error", err)
		}
	}
}

func (m *Monitor) Shutdown(ctx context.Context) {
	if m.cancel == nil {
		return
	}
	m.cancel()
	select {
	case <-m.done:
	case <-ctx.Done():
	}
}
