package database_test

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadOnly_RefusesWritesWhenSessionDefaultIsOff(t *testing.T) {
	dbURL := sharedtest.DatabaseURL(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "SET default_transaction_read_only = off"); err != nil {
		t.Fatalf("drop session default: %v", err)
	}

	err = database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "CREATE TABLE readonly_guard_probe (id int)")
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("CREATE TABLE error = %v, want SQLSTATE 25006", err)
	}

	var got int
	err = database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT 41 + 1").Scan(&got)
	})
	if err != nil || got != 42 {
		t.Fatalf("SELECT = %d, %v, want 42, nil", got, err)
	}
}

func probePool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dbURL := sharedtest.DatabaseURL(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func TestReadOnly_RefusesInsertAndWritingCTEOnAnExistingTable(t *testing.T) {
	ctx, pool := probePool(t)
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS readonly_probe_rows (id int)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS readonly_probe_rows") })

	for name, stmt := range map[string]string{
		"insert":      "INSERT INTO readonly_probe_rows VALUES (1)",
		"writing CTE": "WITH d AS (INSERT INTO readonly_probe_rows VALUES (2) RETURNING 1) SELECT count(*) FROM d",
		"delete":      "DELETE FROM readonly_probe_rows",
	} {
		t.Run(name, func(t *testing.T) {
			err := database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, stmt)
				return err
			})
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
				t.Fatalf("%s error = %v, want SQLSTATE 25006", name, err)
			}
		})
	}

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM readonly_probe_rows").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after refused writes = %d, %v, want 0, nil", rows, err)
	}
}

func TestReadOnly_FnErrorReachesCallerAndRollsBackTheTransaction(t *testing.T) {
	ctx, pool := probePool(t)
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE readonly_probe_rollback (id int)"); err != nil {
		t.Fatalf("create temp: %v", err)
	}
	sentinel := errors.New("caller gave up")

	err := database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO readonly_probe_rollback VALUES (1)"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReadOnly error = %v, want the fn's error", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM readonly_probe_rollback").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after fn failed = %d, %v, want 0, nil (rolled back)", rows, err)
	}
}

func TestReadOnly_PanicInFnReachesCallerAndFreesTheConnection(t *testing.T) {
	ctx, pool := probePool(t)

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = database.ReadOnly(ctx, pool, func(tx pgx.Tx) error {
			panic("boom in fn")
		})
		return nil
	}()
	if recovered != "boom in fn" {
		t.Fatalf("recovered = %v, want the fn's panic to reach the caller", recovered)
	}

	acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var got int
	if err := database.ReadOnly(acquireCtx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(acquireCtx, "SELECT 7").Scan(&got)
	}); err != nil || got != 7 {
		t.Fatalf("ReadOnly after a panic = %d, %v, want 7, nil on the pool's only connection", got, err)
	}
}

func TestReadOnly_CancelledContextNeverRunsFn(t *testing.T) {
	ctx, pool := probePool(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	ran := false
	err := database.ReadOnly(cancelled, pool, func(tx pgx.Tx) error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadOnly error = %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("fn ran on a cancelled context")
	}
}

func TestReadOnly_DeadlineMidQueryReturnsAndFreesTheConnection(t *testing.T) {
	ctx, pool := probePool(t)
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := database.ReadOnly(short, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(short, "SELECT pg_sleep(10)")
		return err
	})
	if err == nil {
		t.Fatal("ReadOnly returned nil for a query cut off by its deadline")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ReadOnly took %s past a 200ms deadline", elapsed)
	}

	next, cancelNext := context.WithTimeout(ctx, 5*time.Second)
	defer cancelNext()
	var got int
	if err := database.ReadOnly(next, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(next, "SELECT 9").Scan(&got)
	}); err != nil || got != 9 {
		t.Fatalf("ReadOnly after a deadline = %d, %v, want 9, nil", got, err)
	}
}
