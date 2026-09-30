package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	observeAlert "altune/go-api/internal/observe/alert"
	observeHandler "altune/go-api/internal/observe/handler"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	notifier := a.alertNotifier(ctx)

	conditions := []observeAlert.Condition{buildDependencyCondition(a.dependencyHealth)}

	if a.cfg.AlertZeroResultThreshold > 0 {
		eventQuery := discoveryPersistence.NewPgxEventStore(a.pool)
		gap, queryFailing := buildCoverageConditions(eventQuery, a.cfg.AlertZeroResultThreshold)
		conditions = append(conditions, gap, queryFailing)
	}

	conditions = append(conditions, a.jobConditions(alertableJobs)...)

	a.alertMonitor = observeAlert.NewMonitor(notifier, 30*time.Second, conditions...).
		WithLeadership(a.leaderContext)
	a.whenLeaderUnlessDisabled(jobAlertMonitor, a.alertMonitor.Start)
}

func (a *App) alertNotifier(ctx context.Context) observeAlert.AlertNotifier {
	if a.cfg.AlertWebhookURL == "" {
		slog.WarnContext(ctx, "alerts are log-only: ALERT_WEBHOOK_URL is not set")
		return observeAlert.NopNotifier{}
	}
	return observeAlert.NewWebhookNotifier(a.cfg.AlertWebhookURL)
}

func buildDependencyCondition(health func(context.Context) observeHandler.DependencyHealth) observeAlert.Condition {
	return observeAlert.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *observeAlert.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &observeAlert.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: observeAlert.SeveritySignal,
			}
		},
	}
}

func dependencyDownMessage(h observeHandler.DependencyHealth) string {
	msg := "dependencies down:"
	for _, name := range downDependencies(h) {
		msg += " " + name
	}
	return msg
}

func downDependencies(h observeHandler.DependencyHealth) []string {
	var names []string
	for _, dep := range []struct {
		name   string
		status observeHandler.DepStatus
	}{{"db", h.DB}, {"redis", h.Redis}, {"auth", h.Auth}} {
		if dep.status == observeHandler.DepDown {
			names = append(names, dep.name)
		}
	}
	return names
}

type coverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discoveryPorts.QueryCount, error)
}

const coverageQueryFailureEscalation = 3

type coverageCheck struct {
	events    coverageEvents
	threshold int
	failures  int
	last      *observeAlert.Alert
}

const coverageWindow = 24 * time.Hour

func buildCoverageConditions(eventQuery coverageEvents, threshold int) (gap, queryFailing observeAlert.Condition) {
	c := &coverageCheck{events: eventQuery, threshold: threshold}
	gap = observeAlert.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = observeAlert.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *observeAlert.Alert {
	since := time.Now().UTC().Add(-coverageWindow)
	total, err := c.events.ZeroResultTotal(ctx, since)
	if err != nil {
		c.failures++
		slog.WarnContext(ctx, "coverage alert query failed", "error", err, "consecutive_failures", c.failures)
		return c.last
	}
	c.failures = 0
	c.last = c.gapVerdict(ctx, since, total)
	return c.last
}

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *observeAlert.Alert {
	if total < c.threshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, c.threshold)
	if rows, err := c.events.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
		msg += fmt.Sprintf("; top query hit %d times", rows[0].Count)
	}
	return &observeAlert.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: observeAlert.SeveritySignal,
	}
}

func (c *coverageCheck) evalQueryFailing(context.Context) *observeAlert.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &observeAlert.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: observeAlert.SeveritySignal,
	}
}

const jobFailureEscalation = 3

func (a *App) jobConditions(names []jobName) []observeAlert.Condition {
	conditions := make([]observeAlert.Condition, 0, len(names))
	for _, name := range names {
		conditions = append(conditions, buildJobCondition(name, a.job(name)))
	}
	return conditions
}

func buildJobCondition(name jobName, jc *jobControl) observeAlert.Condition {
	return observeAlert.Condition{
		Key: "job_failing:" + string(name),
		Eval: func(context.Context) *observeAlert.Alert {
			streak := jc.consecutive.Load()
			if jc.disabled.Load() || streak < jobFailureEscalation {
				return nil
			}
			return &observeAlert.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, streak),
				Severity: observeAlert.SeveritySignal,
			}
		},
	}
}
