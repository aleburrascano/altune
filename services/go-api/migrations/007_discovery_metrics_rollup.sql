-- discovery: Mission Control metrics rollup.
-- The operator console's in-memory counters reset on restart; this table is the
-- durable, week-over-week history. A nightly job upserts one row per (day,
-- metric): zero_result_rate, ctr, tail_noise_top5_avg, searches. user_id is
-- deliberately absent — these are aggregates (no cardinality / privacy cost).
CREATE TABLE IF NOT EXISTS discovery_metrics (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    as_of      DATE NOT NULL,
    metric     TEXT NOT NULL,
    value      DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (as_of, metric)
);

-- Week-over-week reads scan by metric over a date window.
CREATE INDEX IF NOT EXISTS idx_discovery_metrics_metric_date
    ON discovery_metrics (metric, as_of DESC);
