package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	adminAlert "altune/go-api/internal/admin/alert"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

func (a *App) startAlertMonitor(ctx context.Context) {
	var notifier adminAlert.AlertNotifier = adminAlert.NopNotifier{}
	if a.cfg.HasAlertPush() {
		notifier = adminAlert.NewNtfyNotifier(a.cfg.AlertNtfyURL)
	}

	dependencyDown := adminAlert.Condition{
		Key: "dependency_down",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			h := a.dependencyHealth(ctx)
			if h.Healthy() {
				return nil
			}
			msg := "dependencies down:"
			if h.DB == "down" {
				msg += " db"
			}
			if h.Redis == "down" {
				msg += " redis"
			}
			return &adminAlert.Alert{
				Title:    "altune dependency down",
				Message:  msg,
				Severity: adminAlert.SeveritySignal,
			}
		},
	}

	conditions := []adminAlert.Condition{dependencyDown}

	if a.cfg.AlertZeroResultThreshold > 0 {
		eventQuery := discoveryPersistence.NewPgxEventStore(a.pool)
		conditions = append(conditions, buildCoverageCondition(eventQuery, a.cfg.AlertZeroResultThreshold))
	}

	a.alertMonitor = adminAlert.NewMonitor(notifier, 30*time.Second, conditions...)
	a.whenLeader("alert monitor", a.alertMonitor.Start)
}

// coverageEvents is the slice of the discovery event query the coverage-gap
// alert needs: the unbounded true total for the threshold comparison, plus the
// top-N list for display context only.
type coverageEvents interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]discoveryPorts.QueryCount, error)
}

func buildCoverageCondition(eventQuery coverageEvents, threshold int) adminAlert.Condition {
	return adminAlert.Condition{
		Key: "coverage_zero_result",
		Eval: func(ctx context.Context) *adminAlert.Alert {
			since := time.Now().UTC().Add(-24 * time.Hour)
			// The threshold must compare against the true total: ZeroResultQueries
			// caps at the top 1000 distinct normalized queries, so summing it
			// silently undercounts once a window spans more than that many.
			total, err := eventQuery.ZeroResultTotal(ctx, since)
			if err != nil {
				slog.WarnContext(ctx, "coverage alert query failed", "error", err)
				return nil
			}
			if total < threshold {
				return nil
			}
			msg := fmt.Sprintf("zero-result searches in 24h: %d (threshold %d)", total, threshold)
			if rows, err := eventQuery.ZeroResultQueries(ctx, since, 1000); err == nil && len(rows) > 0 {
				msg += fmt.Sprintf("; top query %q (%d)", rows[0].QueryNorm, rows[0].Count)
			}
			return &adminAlert.Alert{
				Title:    "altune discovery coverage gap",
				Message:  msg,
				Severity: adminAlert.SeveritySignal,
			}
		},
	}
}
