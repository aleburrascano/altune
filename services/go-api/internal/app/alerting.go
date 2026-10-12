package app

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"fmt"
	"log/slog"
	"time"

	observeHandler "altune/go-api/internal/observe/adapters/handler"
	observeNotifier "altune/go-api/internal/observe/adapters/notifier"
	observeDomain "altune/go-api/internal/observe/domain"
	observePorts "altune/go-api/internal/observe/ports"
	observeAlert "altune/go-api/internal/observe/service/alert"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	notifier := a.alertNotifier(ctx)

	conditions := []observeDomain.Condition{buildDependencyCondition(a.dependencyHealth)}

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

func (a *App) alertNotifier(ctx context.Context) observePorts.AlertNotifier {
	if a.cfg.AlertWebhookURL == "" {
		slog.WarnContext(ctx, "alerts are log-only: ALERT_WEBHOOK_URL is not set")
		return observeNotifier.NopNotifier{}
	}
	return observeNotifier.NewWebhookNotifier(a.cfg.AlertWebhookURL)
}

func buildDependencyCondition(health func(context.Context) observeHandler.DependencyHealth) observeDomain.Condition {
	return observeDomain.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *observeDomain.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &observeDomain.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: observeDomain.SeveritySignal,
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
	last      *observeDomain.Alert
}

const coverageWindow = 24 * time.Hour

func buildCoverageConditions(eventQuery coverageEvents, threshold int) (gap, queryFailing observeDomain.Condition) {
	c := &coverageCheck{events: eventQuery, threshold: threshold}
	gap = observeDomain.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = observeDomain.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *observeDomain.Alert {
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

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *observeDomain.Alert {
	if total < c.threshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, c.threshold)
	if rows, err := c.events.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
		msg += fmt.Sprintf("; top query hit %d times", rows[0].Count)
	}
	return &observeDomain.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: observeDomain.SeveritySignal,
	}
}

func (c *coverageCheck) evalQueryFailing(context.Context) *observeDomain.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &observeDomain.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: observeDomain.SeveritySignal,
	}
}

const jobFailureEscalation = 3

func (a *App) jobConditions(names []jobName) []observeDomain.Condition {
	conditions := make([]observeDomain.Condition, 0, len(names))
	for _, name := range names {
		conditions = append(conditions, buildJobCondition(name, a.job(name)))
	}
	return conditions
}

func buildJobCondition(name jobName, jc *runloop.JobControl) observeDomain.Condition {
	return observeDomain.Condition{
		Key: "job_failing:" + string(name),
		Eval: func(context.Context) *observeDomain.Alert {
			streak := jc.ConsecutiveFailures()
			if jc.Disabled() || streak < jobFailureEscalation {
				return nil
			}
			return &observeDomain.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, streak),
				Severity: observeDomain.SeveritySignal,
			}
		},
	}
}
