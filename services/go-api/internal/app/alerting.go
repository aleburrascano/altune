package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	adminAlert "altune/go-api/internal/observe/alert"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	notifier := adminAlert.AlertNotifier(adminAlert.NopNotifier{})

	conditions := []adminAlert.Condition{buildDependencyCondition(a.dependencyHealth)}

	if a.cfg.AlertZeroResultThreshold > 0 {
		eventQuery := discoveryPersistence.NewPgxEventStore(a.pool)
		gap, queryFailing := buildCoverageConditions(eventQuery, a.cfg.AlertZeroResultThreshold)
		conditions = append(conditions, gap, queryFailing)
	}

	conditions = append(conditions, a.jobConditions(alertableJobs)...)

	a.alertMonitor = adminAlert.NewMonitor(notifier, 30*time.Second, conditions...).
		WithLeadership(a.leaderContext)
	a.whenLeader(jobAlertMonitor, a.alertMonitor.Start)
}

// buildDependencyCondition returns the dependency_down condition. It fires
// whenever health reports not Healthy(), and its message names every DepDown
// dependency Healthy() evaluated, so the page always says what broke.
func buildDependencyCondition(health func(context.Context) DependencyHealth) adminAlert.Condition {
	return adminAlert.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &adminAlert.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: adminAlert.SeveritySignal,
			}
		},
	}
}

// dependencyDownMessage lists the DepDown dependencies, in the same set
// Healthy() checks.
func dependencyDownMessage(h DependencyHealth) string {
	msg := "dependencies down:"
	for _, name := range h.down() {
		msg += " " + name
	}
	return msg
}

// coverageEvents is the slice of the discovery event query the coverage-gap
// alert needs: the unbounded true total for the threshold comparison, plus the
// top-N list for display context only.
type coverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discoveryPorts.QueryCount, error)
}

// coverageQueryFailureEscalation is how many consecutive failed coverage
// queries (one per monitor tick, ~30s apart) it takes before the failure pages
// on its own key. A single transient blip stays a warning log.
const coverageQueryFailureEscalation = 3

// coverageCheck is the state shared by the coverage-gap condition and its
// query-failure condition. The monitor evaluates conditions sequentially on a
// single goroutine, so the fields need no locking.
type coverageCheck struct {
	events    coverageEvents
	threshold int
	// failures counts consecutive ZeroResultTotal errors; reset on success.
	failures int
	// last is the most recent successfully computed gap verdict, held through
	// a failure streak so a query error never reads as "no gap found".
	last *adminAlert.Alert
}

const coverageWindow = 24 * time.Hour

// buildCoverageConditions returns the coverage-gap condition plus a separate
// condition that fires once the gap query itself keeps failing. The keys are
// distinct so a broken check never looks like a healthy day, and so the
// failure still pages while a gap alert is already firing. queryFailing must be
// registered after gap so it reads the current tick's result.
func buildCoverageConditions(eventQuery coverageEvents, threshold int) (gap, queryFailing adminAlert.Condition) {
	c := &coverageCheck{events: eventQuery, threshold: threshold}
	gap = adminAlert.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = adminAlert.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *adminAlert.Alert {
	since := time.Now().UTC().Add(-coverageWindow)
	// The threshold must compare against the true total: ZeroResultQueries
	// caps at the top 1000 distinct normalized queries, so summing it
	// silently undercounts once a window spans more than that many.
	total, err := c.events.ZeroResultTotal(ctx, since)
	if err != nil {
		c.failures++
		slog.WarnContext(ctx, "coverage alert query failed", "error", err, "consecutive_failures", c.failures)
		// Unknown is not healthy: hold the last verdict instead of resolving.
		return c.last
	}
	c.failures = 0
	c.last = c.gapVerdict(ctx, since, total)
	return c.last
}

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *adminAlert.Alert {
	if total < c.threshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, c.threshold)
	// The alert leaves the system (ntfy), so it carries counts only: the
	// query text itself is user content and must never cross that boundary.
	if rows, err := c.events.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
		msg += fmt.Sprintf("; top query hit %d times", rows[0].Count)
	}
	return &adminAlert.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: adminAlert.SeveritySignal,
	}
}

// evalQueryFailing reports the failure streak recorded by evalGap. It carries
// the count only: the driver error stays in the server logs, never in ntfy.
func (c *coverageCheck) evalQueryFailing(context.Context) *adminAlert.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &adminAlert.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: adminAlert.SeveritySignal,
	}
}

const jobFailureEscalation = 3

var alertableJobs = []jobName{
	jobStalePendingReconcile,
	jobOrphanedAudioReconcile,
	jobBehavioralCorpusRefresh,
	jobDiscographyEventPrune,
	jobVocabularyRefresh,
	jobBehavioralRankingRefresh,
	jobDeletedIdentityErasure,
	jobAcquisitionSourceCanary,
}

func (a *App) jobConditions(names []jobName) []adminAlert.Condition {
	conditions := make([]adminAlert.Condition, 0, len(names))
	for _, name := range names {
		conditions = append(conditions, buildJobCondition(name, a.job(name)))
	}
	return conditions
}

func buildJobCondition(name jobName, jc *jobControl) adminAlert.Condition {
	return adminAlert.Condition{
		Key: "job_failing:" + string(name),
		Eval: func(context.Context) *adminAlert.Alert {
			streak := jc.consecutive.Load()
			if jc.disabled.Load() || streak < jobFailureEscalation {
				return nil
			}
			return &adminAlert.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, streak),
				Severity: adminAlert.SeveritySignal,
			}
		},
	}
}
