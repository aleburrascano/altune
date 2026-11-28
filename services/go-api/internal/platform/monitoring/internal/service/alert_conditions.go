package service

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type AlertSources struct {
	DependencyHealth func(context.Context) domain.DependencyHealth
	Coverage         ports.CoverageEvents
	Sessions         ports.SourceSessions
	Jobs             ports.JobHealthReader
	Requests         ports.RequestLatencyReader
	Clock            clock.Func
}

var ErrClockRequired = errors.New("alerts: Sources.Clock is required")

var ErrRequestsRequired = errors.New("alerts: Sources.Requests is required: the API error ratio is the symptom the page is for")

func AlertConditions(s AlertSources) ([]domain.Condition, error) {
	if s.Clock == nil {
		return nil, ErrClockRequired
	}
	if s.Requests == nil {
		return nil, ErrRequestsRequired
	}
	gap, queryFailing := coverageConditions(s.Coverage, s.Clock)
	conditions := []domain.Condition{
		apiErrorRatioCondition(s.Requests, s.Clock),
		dependencyCondition(s.DependencyHealth), gap, queryFailing,
		youTubeCookiesCondition(s.Sessions),
	}
	for _, name := range s.Jobs.Alertable() {
		conditions = append(conditions, jobCondition(name, s.Jobs))
	}
	return conditions, nil
}

func dependencyCondition(health func(context.Context) domain.DependencyHealth) domain.Condition {
	return domain.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *domain.Alert {
			h := health(ctx)
			if h.Healthy() {
				return nil
			}
			return &domain.Alert{
				Title:    "altune dependency down",
				Message:  dependencyDownMessage(h),
				Severity: domain.SeverityWatch,
			}
		},
	}
}

func dependencyDownMessage(h domain.DependencyHealth) string {
	msg := "dependencies down:"
	for _, dep := range []struct {
		name   string
		status domain.DepStatus
	}{{"db", h.DB}, {"redis", h.Redis}, {"auth", h.Auth}} {
		if dep.status == domain.DepDown {
			msg += " " + dep.name
		}
	}
	return msg
}

const cookieDropInCommand = "cd ~/altune/services/go-api && bash deploy/cookie-jar-dir.sh <export>"

func youTubeCookiesCondition(sessions ports.SourceSessions) domain.Condition {
	return domain.Condition{
		Key: "youtube_cookies_signed_out",
		Eval: func(ctx context.Context) *domain.Alert {
			since, signedOut := sessions.YouTubeSignedOutSince(ctx)
			if !signedOut {
				return nil
			}
			return &domain.Alert{
				Title: "altune YouTube cookies expired",
				Message: fmt.Sprintf("YouTube has asked yt-dlp to sign in since %s, so YouTube downloads fail. "+
					"Export cookies.txt from a browser signed in to YouTube, copy it to the VM and run: %s",
					since.UTC().Format(time.RFC3339), cookieDropInCommand),
				Severity: domain.SeverityWatch,
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
	events   ports.CoverageEvents
	clock    clock.Func
	failures int
	last     *domain.Alert
}

func coverageConditions(events ports.CoverageEvents, clk clock.Func) (gap, queryFailing domain.Condition) {
	c := &coverageCheck{events: events, clock: clk}
	gap = domain.Condition{Key: "coverage_zero_result", Eval: c.evalGap}
	queryFailing = domain.Condition{Key: "coverage_query_failing", Eval: c.evalQueryFailing}
	return gap, queryFailing
}

func (c *coverageCheck) evalGap(ctx context.Context) *domain.Alert {
	since := c.clock.UTC().Add(-coverageWindow)
	total, err := c.events.ZeroResultTotal(ctx, since)
	if err != nil {
		c.failures++
		slog.WarnContext(ctx, "coverage alert query failed", logging.KeyError, err, "consecutive_failures", c.failures)
		return c.last
	}
	c.failures = 0
	c.last = c.gapVerdict(ctx, since, total)
	return c.last
}

func (c *coverageCheck) gapVerdict(ctx context.Context, since time.Time, total int) *domain.Alert {
	if total < coverageZeroResultThreshold {
		return nil
	}
	msg := fmt.Sprintf("zero-result searches in %dh: %d (threshold %d)", int(coverageWindow.Hours()), total, coverageZeroResultThreshold)
	if hits, found := c.events.TopNoResultQueryHits(ctx, since); found {
		msg += fmt.Sprintf("; top query hit %d times", hits)
	}
	return &domain.Alert{
		Title:    "altune discovery coverage gap",
		Message:  msg,
		Severity: domain.SeverityWatch,
	}
}

func (c *coverageCheck) evalQueryFailing(context.Context) *domain.Alert {
	if c.failures < coverageQueryFailureEscalation {
		return nil
	}
	return &domain.Alert{
		Title:    "altune coverage alert check failing",
		Message:  fmt.Sprintf("coverage-gap query failed %d consecutive times; gap status unknown", c.failures),
		Severity: domain.SeverityWatch,
	}
}

const jobFailureEscalation = 3

func jobCondition(name string, jobs ports.JobHealthReader) domain.Condition {
	return domain.Condition{
		Key: "job_failing:" + name,
		Eval: func(context.Context) *domain.Alert {
			state := jobs.State(name)
			if state.Disabled || state.ConsecutiveFailures < jobFailureEscalation {
				return nil
			}
			return &domain.Alert{
				Title:    "altune background job failing",
				Message:  fmt.Sprintf("job %q failed %d consecutive runs", name, state.ConsecutiveFailures),
				Severity: domain.SeverityWatch,
			}
		},
	}
}
