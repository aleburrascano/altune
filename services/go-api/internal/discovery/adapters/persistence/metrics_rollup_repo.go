package persistence

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.MetricsRollupStore = (*PgxMetricsRollup)(nil)

// PgxMetricsRollup computes the daily Mission Control metrics from the event
// stream and persists them to discovery_metrics for restart-surviving,
// week-over-week history.
type PgxMetricsRollup struct {
	pool *pgxpool.Pool
}

func NewPgxMetricsRollup(pool *pgxpool.Pool) *PgxMetricsRollup {
	return &PgxMetricsRollup{pool: pool}
}

// RollupDay computes the UTC day's metrics in one CTE and upserts the four rows
// (idempotent on (as_of, metric)). Read-only over discovery_events; aggregates
// only, no user_id. Payload casts are guarded by jsonb_typeof (inside CASE so
// the guard is evaluated before the cast): a poisoned client payload
// ("zero_result":"abc") is skipped for that row, never a 22P02 that fails the
// whole rollup.
func (r *PgxMetricsRollup) RollupDay(ctx context.Context, day time.Time) error {
	dayStart := day.UTC().Truncate(24 * time.Hour)
	_, err := r.pool.Exec(ctx,
		`WITH d AS (
			SELECT
				COUNT(*) FILTER (WHERE event_type = 'search_performed') AS searches,
				COUNT(*) FILTER (WHERE event_type = 'search_performed'
					AND CASE WHEN jsonb_typeof(payload->'zero_result') = 'boolean'
						THEN (payload->>'zero_result')::boolean ELSE false END) AS zero,
				COUNT(DISTINCT search_id) FILTER (WHERE event_type = 'result_clicked') AS clicked,
				AVG(CASE WHEN jsonb_typeof(payload->'tail_noise_top5') = 'number'
					THEN (payload->>'tail_noise_top5')::numeric END)
					FILTER (WHERE event_type = 'search_performed') AS tail_avg
			FROM discovery_events
			WHERE occurred_at >= $1 AND occurred_at < $1 + interval '1 day'
		)
		INSERT INTO discovery_metrics (as_of, metric, value)
		SELECT $1::date, 'zero_result_rate',
			CASE WHEN searches > 0 THEN zero::float8 / searches ELSE 0 END FROM d
		UNION ALL SELECT $1::date, 'ctr',
			CASE WHEN searches > 0 THEN clicked::float8 / searches ELSE 0 END FROM d
		UNION ALL SELECT $1::date, 'tail_noise_top5_avg', COALESCE(tail_avg, 0)::float8 FROM d
		UNION ALL SELECT $1::date, 'searches', searches::float8 FROM d
		ON CONFLICT (as_of, metric) DO UPDATE SET value = EXCLUDED.value, created_at = now()`,
		dayStart,
	)
	if err != nil {
		return fmt.Errorf("rollup discovery metrics for %s: %w", dayStart.Format("2006-01-02"), err)
	}
	return nil
}

// MetricsHistory returns the last `days` daily values of a metric, newest first.
func (r *PgxMetricsRollup) MetricsHistory(ctx context.Context, metric string, days int) ([]ports.MetricPoint, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT as_of, value
		FROM discovery_metrics
		WHERE metric = $1
		ORDER BY as_of DESC
		LIMIT $2`,
		metric, days,
	)
	if err != nil {
		return nil, fmt.Errorf("query metrics history %q: %w", metric, err)
	}
	defer rows.Close()

	points := []ports.MetricPoint{}
	for rows.Next() {
		var p ports.MetricPoint
		if err := rows.Scan(&p.AsOf, &p.Value); err != nil {
			return nil, fmt.Errorf("scan metric point: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}
