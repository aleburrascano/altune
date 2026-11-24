package persistence_test

import (
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func tracedPool(t *testing.T, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	dbURL := sharedtest.DatabaseURL(t)
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
