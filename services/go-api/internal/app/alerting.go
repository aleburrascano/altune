package app

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"time"
)

const alertCheckInterval = 30 * time.Second

func (a *application) startAlertMonitor(ctx context.Context) error {
	notifier := alerts.NewNotifier(ctx, a.cfg)
	conditions, err := a.alertConditions(search.NewPostgresEventStore(a.pool, clock.Now))
	if err != nil {
		return err
	}
	monitor, err := newAlertMonitor(notifier, alertCheckInterval, conditions)
	if err != nil {
		return err
	}
	a.alertMonitor = monitor
	a.jobs.Ticker(jobs.AlertMonitor, alertCheckInterval, monitor.Pass)
	return nil
}

func newAlertMonitor(notifier monitoring.AlertNotifier, interval time.Duration, conditions []monitoring.Condition) (*monitoring.AlertMonitor, error) {
	monitor, err := monitoring.NewAlertMonitor(notifier, interval, clock.Now, conditions...)
	if err != nil {
		return nil, fmt.Errorf("alert monitor: %w", err)
	}
	return monitor, nil
}

func (a *application) alertConditions(coverage alerts.CoverageEvents) ([]monitoring.Condition, error) {
	r0, err := alerts.Conditions(alerts.Sources{
		DependencyHealth:      a.dependencyHealth,
		Coverage:              coverage,
		YouTubeSignedOutSince: a.download.YouTubeSignedOutSince,
		Jobs:                  &a.jobs,
		Requests:              a.latency.Snapshot,
		Clock:                 clock.Now,
	})
	if err != nil {
		return r0, fmt.Errorf("conditions: %w", err)
	}
	return r0, nil
}
