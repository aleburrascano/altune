package dbtest_test

import (
	"altune/go-api/internal/shared/dbtest"
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func sharedDatabaseURL(t *testing.T) string {
	t.Helper()
	dbtest.RequireIntegration(t)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("INTEGRATION=1 needs DATABASE_URL pointing at a migrated Postgres")
	}
	return url
}

func TestPool_ReachesTheIntegrationDatabase(t *testing.T) {
	pool := dbtest.Pool(t)

	var got int
	if err := pool.QueryRow(context.Background(), "SELECT 40 + 2").Scan(&got); err != nil || got != 42 {
		t.Fatalf("SELECT = %d, %v, want 42, nil", got, err)
	}
}

func TestTx_RollsBackWhatTheTestWroteWhenItEnds(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	table := pgx.Identifier{"sharedtest_tx_probe_" + uuid.NewString()[:8]}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+table+" (id int)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP TABLE "+table) })

	t.Run("writer", func(t *testing.T) {
		tx := dbtest.Tx(t)
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
	url := dbtest.DatabaseURL(t)

	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse DatabaseURL: %v", err)
	}
	var database string
	if err := dbtest.Pool(t).QueryRow(context.Background(), "SELECT current_database()").Scan(&database); err != nil || database != cfg.Database {
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
	pool := dbtest.TracedPool(t, tracer)

	var got int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&got); err != nil || got != 1 {
		t.Fatalf("SELECT = %d, %v, want 1, nil", got, err)
	}
	if tracer.started != 1 {
		t.Fatalf("tracer saw %d queries, want 1", tracer.started)
	}
}

func TestPool_RowsAPackageWritesAreInvisibleToTheSharedDatabase(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status)
		 VALUES ($1, $2, 'isolation probe', 'probe', $3, 'pending')`,
		id, uuid.New(), uuid.NewString()); err != nil {
		t.Fatalf("insert a claimable track: %v", err)
	}

	shared, err := pgx.Connect(ctx, sharedDatabaseURL(t))
	if err != nil {
		t.Fatalf("connect to the shared database: %v", err)
	}
	t.Cleanup(func() { _ = shared.Close(context.Background()) })

	var seen int
	if err := shared.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id = $1`, id).Scan(&seen); err != nil {
		t.Fatalf("look for the track in the shared schema: %v", err)
	}
	if seen != 0 {
		t.Errorf("the shared database sees %d rows a test package wrote, want 0", seen)
	}
}

func schemaExists(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()
	var found bool
	if err := conn.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, name).Scan(&found); err != nil {
		t.Fatalf("look for schema %s: %v", name, err)
	}
	return found
}

func TestPool_ANewPackageDropsTestSchemasOlderThanSixHoursAndKeepsRecentOnes(t *testing.T) {
	ctx := context.Background()
	shared, err := pgx.Connect(ctx, sharedDatabaseURL(t))
	if err != nil {
		t.Fatalf("connect to the shared database: %v", err)
	}
	t.Cleanup(func() { _ = shared.Close(context.Background()) })

	stale := fmt.Sprintf("testdb_%d_1", time.Now().Add(-7*time.Hour).Unix())
	recent := fmt.Sprintf("testdb_%d_2", time.Now().Add(-time.Hour).Unix())
	for _, name := range []string{stale, recent} {
		if _, err := shared.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatalf("create schema %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for _, name := range []string{stale, recent} {
			_, _ = shared.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{name}.Sanitize()+" CASCADE")
		}
	})

	probe := exec.Command(os.Args[0], "-test.run=^TestPool_ReachesTheIntegrationDatabase$")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("a fresh test process failed: %v\n%s", err, out)
	}

	if schemaExists(t, shared, stale) {
		t.Errorf("schema %s is older than six hours and survived a new package's setup", stale)
	}
	if !schemaExists(t, shared, recent) {
		t.Errorf("schema %s is recent and was dropped", recent)
	}
}
