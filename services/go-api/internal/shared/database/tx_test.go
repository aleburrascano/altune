package database_test

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func txProbeTable(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, pool := probePool(t)
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE intx_probe (id int UNIQUE DEFERRABLE INITIALLY DEFERRED)"); err != nil {
		t.Fatalf("create temp: %v", err)
	}
	return ctx, pool
}

func storedProbeRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM intx_probe").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	return rows
}

func insertProbeRow(ctx context.Context, id int) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO intx_probe VALUES ($1)", id)
		return err
	}
}

func TestInTx_CommitsWhatFnWrote(t *testing.T) {
	ctx, pool := txProbeTable(t)

	if err := database.InTx(ctx, pool, insertProbeRow(ctx, 1)); err != nil {
		t.Fatalf("InTx: %v", err)
	}

	if got := storedProbeRows(ctx, t, pool); got != 1 {
		t.Fatalf("rows after commit = %d, want 1", got)
	}
}

func TestInTx_FnErrorRollsBackAndReachesCaller(t *testing.T) {
	ctx, pool := txProbeTable(t)
	sentinel := errors.New("second write refused")

	err := database.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := insertProbeRow(ctx, 1)(tx); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx error = %v, want the fn's error", err)
	}
	if got := storedProbeRows(ctx, t, pool); got != 0 {
		t.Fatalf("rows after fn failed = %d, want 0 (rolled back)", got)
	}
}

func TestInTx_PanicRollsBackReachesCallerAndFreesTheConnection(t *testing.T) {
	ctx, pool := txProbeTable(t)

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = database.InTx(ctx, pool, func(tx pgx.Tx) error {
			if err := insertProbeRow(ctx, 1)(tx); err != nil {
				return err
			}
			panic("boom after a write")
		})
		return nil
	}()

	if recovered != "boom after a write" {
		t.Fatalf("recovered = %v, want the fn's panic to reach the caller", recovered)
	}
	acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if got := storedProbeRows(acquireCtx, t, pool); got != 0 {
		t.Fatalf("rows after panic = %d, want 0 (rolled back on the pool's only connection)", got)
	}
}

func TestInTx_CommitConflictReachesCallerAndKeepsNothing(t *testing.T) {
	ctx, pool := txProbeTable(t)

	err := database.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := insertProbeRow(ctx, 7)(tx); err != nil {
			return err
		}
		return insertProbeRow(ctx, 7)(tx)
	})

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("InTx error = %v, want the deferred unique violation 23505 from commit", err)
	}
	if got := storedProbeRows(ctx, t, pool); got != 0 {
		t.Fatalf("rows after failed commit = %d, want 0", got)
	}
}

func TestInTx_WritesEvenWhenTheSessionDefaultsToReadOnly(t *testing.T) {
	ctx, pool := txProbeTable(t)
	if _, err := pool.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		t.Fatalf("set session default: %v", err)
	}

	if err := database.InTx(ctx, pool, insertProbeRow(ctx, 1)); err != nil {
		t.Fatalf("InTx under a read-only session default: %v", err)
	}

	if got := storedProbeRows(ctx, t, pool); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

func TestInTx_CancelledContextNeverRunsFn(t *testing.T) {
	ctx, pool := txProbeTable(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	ran := false
	err := database.InTx(cancelled, pool, func(tx pgx.Tx) error {
		ran = true
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("InTx error = %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("fn ran on a cancelled context")
	}
}
