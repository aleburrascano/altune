// Package alert runs the Mission Control in-process alert monitor: it evaluates
// operator-relevant conditions on a ticker and pushes a notification when one
// transitions into a firing state. It implements the data-consistency
// Fix→Log→Signal cascade — only Signal-tier conditions page the operator.
//
// The monitor cannot observe the box being fully down (it dies with the box);
// that gap is covered off-box by the U9 uptime check.
package alert

import (
	"context"
	"log/slog"
	"time"
)

// Severity follows the Fix→Log→Signal cascade. Only Signal pages the operator.
type Severity int

const (
	SeverityFix    Severity = iota // self-healed; not surfaced
	SeverityLog                    // recorded; not paged
	SeveritySignal                 // page the operator
)

// Alert is the payload pushed to the operator. Its Message MUST carry only
// operational state names — never connection strings, hostnames, or user ids.
type Alert struct {
	Title    string
	Message  string
	Severity Severity
}

// AlertNotifier delivers an alert out of band. Defined here, where it is
// consumed, per "interfaces belong to consumers".
type AlertNotifier interface {
	Notify(ctx context.Context, a Alert) error
}

// Condition is one monitored signal. Eval returns a non-nil Alert while the
// condition is firing and nil while healthy; Key identifies the incident so the
// monitor can dedup (fire once per incident, reset on recovery).
type Condition struct {
	Key  string
	Eval func(ctx context.Context) *Alert
}

// Monitor evaluates its conditions on a ticker in a single goroutine, so its
// incident-tracking state needs no locking.
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

// Start launches the monitor loop. Call Shutdown to stop it.
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
			continue // already paged for this incident; don't spam
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

// Shutdown stops the monitor loop, waiting up to the context deadline.
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
