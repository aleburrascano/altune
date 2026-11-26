package app

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/app/downloadsetup"
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
	monitor, err := newAlertMonitor(&a.jobs, notifier, alertCheckInterval, conditions)
	if err != nil {
		return err
	}
	a.alertMonitor = monitor
	a.jobs.WhenLeader(jobs.AlertMonitor, a.alertMonitor.Start)
	return nil
}

func newAlertMonitor(runner *jobs.Runner, notifier monitoring.AlertNotifier, interval time.Duration, conditions []monitoring.Condition) (*monitoring.AlertMonitor, error) {
	monitor, err := monitoring.NewAlertMonitor(notifier, interval, clock.Now, clock.NewTicker, runner.LeaderContextFor(jobs.AlertMonitor), conditions...)
	if err != nil {
		return nil, fmt.Errorf("alert monitor: %w", err)
	}
	return monitor, nil
}

func (a *application) alertConditions(coverage alerts.CoverageEvents) ([]monitoring.Condition, error) {
	return alerts.Conditions(alerts.Sources{
		DependencyHealth:      a.dependencyHealth,
		Coverage:              coverage,
		YouTubeSignedOutSince: downloadsetup.YtDlpSearcher(a.cfg).SignedOutSince,
		Jobs:                  &a.jobs,
		Clock:                 clock.Now,
	})
}
