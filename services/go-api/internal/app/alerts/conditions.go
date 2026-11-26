package alerts

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type CoverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	NoResultSearches(ctx context.Context, since time.Time, limit int) ([]search.QueryCount, error)
}

type Sources struct {
	DependencyHealth      func(context.Context) monitoring.DependencyHealth
	Coverage              CoverageEvents
	YouTubeSignedOutSince func(context.Context) (time.Time, bool)
	Jobs                  *jobs.Runner
}

func Conditions(s Sources) []monitoring.Condition {
	gap, queryFailing := coverageConditions(s.Coverage)
	conditions := []monitoring.Condition{
		dependencyCondition(s.DependencyHealth), gap, queryFailing,
		youTubeCookiesCondition(s.YouTubeSignedOutSince),
	}
	for _, name := range jobs.Alertable() {
		conditions = append(conditions, jobCondition(name, s.Jobs))
	}
	return conditions
}

func dependencyCondition(health func(context.Context) monitoring.DependencyHealth) monitoring.Condition {
	return monitoring.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *monitoring.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &monitoring.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: monitoring.SeverityWatch,
			}
		},
	}
}

func dependencyDownMessage(h monitoring.DependencyHealth) string {
	msg := "dependencies down:"
	for _, dep := range []struct {
		name   string
		status monitoring.DepStatus
	}{{"db", h.DB}, {"redis", h.Redis}, {"auth", h.Auth}} {
		if dep.status == monitoring.DepDown {
			msg += " " + dep.name
		}
	}
	return msg
}

const cookieDropInCommand = "cd ~/altune/services/go-api && bash deploy/cookie-jar-dir.sh <export>"

func youTubeCookiesCondition(signedOutSince func(context.Context) (time.Time, bool)) monitoring.Condition {
	return monitoring.Condition{
		Key: "youtube_cookies_signed_out",
		Eval: func(ctx context.Context) *monitoring.Alert {
			since, signedOut := signedOutSince(ctx)
			if !signedOut {
				return nil
			}
			return &monitoring.Alert{
				Title: "altune YouTube cookies expired",
				Message: fmt.Sprintf("YouTube has asked yt-dlp to sign in since %s, so YouTube downloads fail. "+
					"Export cookies.txt from a browser signed in to YouTube, copy it to the VM and run: %s",
					since.UTC().Format(time.RFC3339), cookieDropInCommand),
				Severity: monitoring.SeverityWatch,
			}
		},
	}
}

const (
	coverageWindow                 = 24 * time.Hour
	coverageZeroResultThreshold    = 25
	coverageQueryFailureEscalation = 3
)

type coverageCheck struct {
	events   CoverageEvents
	failures int
	last     *monitoring.Alert
}

func coverageConditions(events CoverageEvents) (gap, queryFailing monitoring.Condition) {
	c := &coverageCheck{events: events}
	gap = monitoring.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = monitoring.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *monitoring.Alert {
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

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *monitoring.Alert {
	if total < coverageZeroResultThreshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, coverageZeroResultThreshold)
	if rows, err := c.events.NoResultSearches(ctx, since, 1000); err == nil && len(rows) > 0 {
		msg += fmt.Sprintf("; top query hit %d times", rows[0].Count)
	}
	return &monitoring.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: monitoring.SeverityWatch,
	}
}

func (c *coverageCheck) evalQueryFailing(context.Context) *monitoring.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &monitoring.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: monitoring.SeverityWatch,
	}
}

const jobFailureEscalation = 3

func jobCondition(name jobs.Name, runner *jobs.Runner) monitoring.Condition {
	jc := runner.Control(name)
	return monitoring.Condition{
		Key: "job_failing:" + string(name),
		Eval: func(context.Context) *monitoring.Alert {
			streak := jc.ConsecutiveFailures()
			if jc.Disabled() || streak < jobFailureEscalation {
				return nil
			}
			return &monitoring.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, streak),
				Severity: monitoring.SeverityWatch,
			}
		},
	}
}
