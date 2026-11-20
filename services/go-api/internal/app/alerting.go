package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/runloop"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	notifier := a.alertNotifier(ctx)
	conditions := a.alertConditions(discovery.NewPgxEventStore(a.pool))
	a.alertMonitor = observe.NewAlertMonitor(notifier, 30*time.Second, conditions...).
		WithLeadership(a.leaderContext)
	a.whenLeader(jobAlertMonitor, a.alertMonitor.Start)
}

func (a *App) alertConditions(events coverageEvents) []observe.Condition {
	gap, queryFailing := buildCoverageConditions(events)
	conditions := []observe.Condition{buildDependencyCondition(a.dependencyHealth), gap, queryFailing}
	return append(conditions, a.jobConditions(alertableJobs)...)
}

func (a *App) alertNotifier(ctx context.Context) observe.AlertNotifier {
	if a.cfg.HasAlertEmail() {
		return observe.NewEmailNotifier(a.cfg.ResendAPIKey, a.cfg.AlertEmailTo, http.DefaultTransport)
	}
	if a.cfg.AlertWebhookURL != "" {
		return observe.NewWebhookNotifier(a.cfg.AlertWebhookURL)
	}
	slog.WarnContext(ctx, "alerts are log-only: neither ALERT_WEBHOOK_URL nor RESEND_API_KEY with ALERT_EMAIL_TO is set")
	return observe.NopNotifier{}
}

func buildDependencyCondition(health func(context.Context) observe.DependencyHealth) observe.Condition {
	return observe.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *observe.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &observe.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: observe.SeveritySignal,
			}
		},
	}
}

func dependencyDownMessage(h observe.DependencyHealth) string {
	msg := "dependencies down:"
	for _, name := range downDependencies(h) {
		msg += " " + name
	}
	return msg
}

func downDependencies(h observe.DependencyHealth) []string {
	var names []string
	for _, dep := range []struct {
		name   string
		status observe.DepStatus
	}{{"db", h.DB}, {"redis", h.Redis}, {"auth", h.Auth}} {
		if dep.status == observe.DepDown {
			names = append(names, dep.name)
		}
	}
	return names
}

type coverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discovery.QueryCount, error)
}

const coverageQueryFailureEscalation = 3

const coverageZeroResultThreshold = 25

type coverageCheck struct {
	events   coverageEvents
	failures int
	last     *observe.Alert
}

const coverageWindow = 24 * time.Hour

func buildCoverageConditions(eventQuery coverageEvents) (gap, queryFailing observe.Condition) {
	c := &coverageCheck{events: eventQuery}
	gap = observe.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = observe.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *observe.Alert {
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

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *observe.Alert {
	if total < coverageZeroResultThreshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, coverageZeroResultThreshold)
	if rows, err := c.events.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
		msg += fmt.Sprintf("; top query hit %d times", rows[0].Count)
	}
	return &observe.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: observe.SeveritySignal,
	}
}

func (c *coverageCheck) evalQueryFailing(context.Context) *observe.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &observe.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: observe.SeveritySignal,
	}
}

const jobFailureEscalation = 3

func (a *App) jobConditions(names []jobName) []observe.Condition {
	conditions := make([]observe.Condition, 0, len(names))
	for _, name := range names {
		conditions = append(conditions, buildJobCondition(name, a.job(name)))
	}
	return conditions
}

func buildJobCondition(name jobName, jc *runloop.JobControl) observe.Condition {
	return observe.Condition{
		Key: "job_failing:" + string(name),
		Eval: func(context.Context) *observe.Alert {
			streak := jc.ConsecutiveFailures()
			if jc.Disabled() || streak < jobFailureEscalation {
				return nil
			}
			return &observe.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, streak),
				Severity: observe.SeveritySignal,
			}
		},
	}
}
