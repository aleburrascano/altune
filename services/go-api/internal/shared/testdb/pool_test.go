package testdb_test

import (
	"altune/go-api/internal/shared/testdb"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestPool_ReachesTheIntegrationDatabase(t *testing.T) {
	pool := testdb.Pool(t)

	var got int
	if err := pool.QueryRow(context.Background(), "SELECT 40 + 2").Scan(&got); err != nil || got != 42 {
		t.Fatalf("SELECT = %d, %v, want 42, nil", got, err)
	}
}

func TestTx_RollsBackWhatTheTestWroteWhenItEnds(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	table := pgx.Identifier{"sharedtest_tx_probe_" + uuid.NewString()[:8]}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+table+" (id int)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP TABLE "+table) })

	t.Run("writer", func(t *testing.T) {
		tx := testdb.Tx(t)
		if _, err := tx.Exec(ctx, "INSERT INTO "+table+" VALUES (1)"); err != nil {
			t.Fatalf("insert: %v", err)
		}
		var seen int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&seen); err != nil || seen != 1 {
			t.Fatalf("rows inside the transaction = %d, %v, want 1", seen, err)
		}
	})

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after the test ended = %d, %v, want 0 (rolled back)", rows, err)
	}
}

func TestDatabaseURL_IsTheURLPoolConnectsTo(t *testing.T) {
	url := testdb.DatabaseURL(t)

	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse DatabaseURL: %v", err)
	}
	var database string
	if err := testdb.Pool(t).QueryRow(context.Background(), "SELECT current_database()").Scan(&database); err != nil || database != cfg.Database {
		t.Fatalf("Pool's database = %q, %v, want %q from DatabaseURL", database, err, cfg.Database)
	}
}

type countingTracer struct{ started int }

func (c *countingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.started++
	return ctx
}

func (c *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestTracedPool_ReportsEveryQueryToTheTracer(t *testing.T) {
	tracer := &countingTracer{}
	pool := testdb.TracedPool(t, tracer)

	var got int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&got); err != nil || got != 1 {
		t.Fatalf("SELECT = %d, %v, want 1, nil", got, err)
	}
	if tracer.started != 1 {
		t.Fatalf("tracer saw %d queries, want 1", tracer.started)
	}
}
