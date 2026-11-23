package alerts

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type CoverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discovery.QueryCount, error)
}

type Sources struct {
	DependencyHealth      func(context.Context) observe.DependencyHealth
	Coverage              CoverageEvents
	YouTubeSignedOutSince func(context.Context) (time.Time, bool)
	Jobs                  *jobs.Runner
}

func Conditions(s Sources) []observe.Condition {
	gap, queryFailing := coverageConditions(s.Coverage)
	conditions := []observe.Condition{
		dependencyCondition(s.DependencyHealth), gap, queryFailing,
		youTubeCookiesCondition(s.YouTubeSignedOutSince),
	}
	for _, name := range jobs.Alertable() {
		conditions = append(conditions, jobCondition(name, s.Jobs))
	}
	return conditions
}

func dependencyCondition(health func(context.Context) observe.DependencyHealth) observe.Condition {
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
	for _, dep := range []struct {
		name   string
		status observe.DepStatus
	}{{"db", h.DB}, {"redis", h.Redis}, {"auth", h.Auth}} {
		if dep.status == observe.DepDown {
			msg += " " + dep.name
		}
	}
	return msg
}

const cookieDropInCommand = "cd ~/altune/services/go-api && bash deploy/cookie-jar-dir.sh <export>"

func youTubeCookiesCondition(signedOutSince func(context.Context) (time.Time, bool)) observe.Condition {
	return observe.Condition{
		Key: "youtube_cookies_signed_out",
		Eval: func(ctx context.Context) *observe.Alert {
			since, signedOut := signedOutSince(ctx)
			if !signedOut {
				return nil
			}
			return &observe.Alert{
				Title: "altune YouTube cookies expired",
				Message: fmt.Sprintf("YouTube has asked yt-dlp to sign in since %s, so YouTube downloads fail. "+
					"Export cookies.txt from a browser signed in to YouTube, copy it to the VM and run: %s",
					since.UTC().Format(time.RFC3339), cookieDropInCommand),
				Severity: observe.SeveritySignal,
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
	last     *observe.Alert
}

func coverageConditions(events CoverageEvents) (gap, queryFailing observe.Condition) {
	c := &coverageCheck{events: events}
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

func jobCondition(name jobs.Name, runner *jobs.Runner) observe.Condition {
	jc := runner.Control(name)
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
