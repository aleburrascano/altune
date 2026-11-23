package app

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/app/audioacquisition"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
	"time"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	notifier := alerts.NewNotifier(ctx, a.cfg)
	conditions := a.alertConditions(discovery.NewPgxEventStore(a.pool))
	a.alertMonitor = observe.NewAlertMonitor(notifier, 30*time.Second, conditions...).
		WithLeadership(a.jobs.LeaderContext)
	a.jobs.WhenLeader(jobs.AlertMonitor, a.alertMonitor.Start)
}

func (a *App) alertConditions(coverage alerts.CoverageEvents) []observe.Condition {
	return alerts.Conditions(alerts.Sources{
		DependencyHealth:      a.dependencyHealth,
		Coverage:              coverage,
		YouTubeSignedOutSince: audioacquisition.YtDlpSearcher(a.cfg).SignedOutSince,
		Jobs:                  &a.jobs,
	})
}
