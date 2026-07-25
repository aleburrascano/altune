package persistence

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.MetricsRollupStore = (*PgxMetricsRollup)(nil)

type PgxMetricsRollup struct {
	pool *pgxpool.Pool
}

func NewPgxMetricsRollup(pool *pgxpool.Pool) *PgxMetricsRollup {
	return &PgxMetricsRollup{pool: pool}
}

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

func (r *PgxMetricsRollup) RecordMetrics(ctx context.Context, day time.Time, values map[string]float64) error {
	if len(values) == 0 {
		return nil
	}
	dayStart := day.UTC().Truncate(24 * time.Hour)

	names := make([]string, 0, len(values))
	nums := make([]float64, 0, len(values))
	for name, value := range values {
		names = append(names, name)
		nums = append(nums, value)
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO discovery_metrics (as_of, metric, value)
		SELECT $1::date, m.name, m.value
		FROM unnest($2::text[], $3::float8[]) AS m(name, value)
		ON CONFLICT (as_of, metric) DO UPDATE SET value = EXCLUDED.value, created_at = now()`,
		dayStart, names, nums,
	)
	if err != nil {
		return fmt.Errorf("record %d discovery metrics for %s: %w", len(values), dayStart.Format("2006-01-02"), err)
	}
	return nil
}

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
