package dbtest

import (
	"altune/go-api/internal/shared/database"
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func DatabaseURLWithoutSchema(t testing.TB) string {
	t.Helper()
	RequireIntegration(t)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("INTEGRATION=1 needs DATABASE_URL pointing at a migrated Postgres (scripts/ci/postgres.sh starts one)")
	}
	return url
}

func DatabaseURL(t testing.TB) string {
	t.Helper()
	return requireSchemaURL(t, DatabaseURLWithoutSchema(t))
}

func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := database.NewPool(context.Background(), DatabaseURL(t), 0)
	if err != nil {
		t.Fatalf("connect to DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TracedPool(t testing.TB, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(DatabaseURL(t))
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect to DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func Tx(t testing.TB) pgx.Tx {
	t.Helper()
	tx, err := Pool(t).Begin(context.Background())
	if err != nil {
		t.Fatalf("begin test transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}
