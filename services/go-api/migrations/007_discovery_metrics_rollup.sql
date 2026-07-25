CREATE TABLE IF NOT EXISTS discovery_metrics (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    as_of      DATE NOT NULL,
    metric     TEXT NOT NULL,
    value      DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (as_of, metric)
);

CREATE INDEX IF NOT EXISTS idx_discovery_metrics_metric_date
    ON discovery_metrics (metric, as_of DESC);
