package app

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/app/downloadsetup"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
	"fmt"
	"time"
)

func (a *App) startAlertMonitor(ctx context.Context) error {
	notifier := alerts.NewNotifier(ctx, a.cfg)
	conditions, err := a.alertConditions(search.NewPostgresEventStore(a.pool))
	if err != nil {
		return err
	}
	monitor, err := monitoring.NewAlertMonitor(notifier, 30*time.Second, conditions...)
	if err != nil {
		return fmt.Errorf("alert monitor: %w", err)
	}
	a.alertMonitor = monitor.WithLeadership(a.jobs.LeaderContext)
	a.jobs.WhenLeader(jobs.AlertMonitor, a.alertMonitor.Start)
	return nil
}

func (a *App) alertConditions(coverage alerts.CoverageEvents) ([]monitoring.Condition, error) {
	return alerts.Conditions(alerts.Sources{
		DependencyHealth:      a.dependencyHealth,
		Coverage:              coverage,
		YouTubeSignedOutSince: downloadsetup.YtDlpSearcher(a.cfg).SignedOutSince,
		Jobs:                  &a.jobs,
		Clock:                 time.Now,
	})
}
