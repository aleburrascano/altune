package database_test

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIsTransient_ErrorShapes(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"no error":                          {nil, false},
		"plain failure":                     {errors.New("boom"), false},
		"no rows":                           {pgx.ErrNoRows, false},
		"closed transaction":                {pgx.ErrTxClosed, false},
		"connection closed mid-read":        {fmt.Errorf("read: %w", io.EOF), true},
		"connection cut mid-message":        {fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		"socket reset":                      {&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true},
		"deadline":                          {fmt.Errorf("query: %w", context.DeadlineExceeded), true},
		"connection already closed":         {fmt.Errorf("query: %w", pgconn.ErrConnClosed), true},
		"caller cancelled":                  {fmt.Errorf("query: %w", context.Canceled), false},
		"caller cancelled on a closed conn": {fmt.Errorf("%w: %w", context.Canceled, pgconn.ErrConnClosed), false},
		"caller cancelled as the conn cut":  {fmt.Errorf("%w: %w", context.Canceled, io.EOF), false},
		"serialization failure":             {&pgconn.PgError{Code: "40001"}, true},
		"too many connections":              {fmt.Errorf("x: %w", &pgconn.PgError{Code: "53300"}), true},
		"unique violation":                  {&pgconn.PgError{Code: "23505"}, false},
		"server error beats a deadline":     {fmt.Errorf("%w: %w", &pgconn.PgError{Code: "23505"}, context.DeadlineExceeded), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := database.IsTransient(tc.err); got != tc.want {
				t.Fatalf("IsTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransient_RefusedConnectionIsTransient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@"+addr+"/db?sslmode=disable&connect_timeout=5")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	err = pool.Ping(context.Background())

	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) || !database.IsTransient(err) {
		t.Fatalf("refused connect error = %v (connect error: %v), want a transient *pgconn.ConnectError", err, errors.As(err, &connectErr))
	}
}

func TestIsTransient_AConnectThatFailsForAnyReasonIsTransient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go answerEverySSLRequestWithNo(ln)
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@"+ln.Addr().String()+"/db?sslmode=require&connect_timeout=5")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	err = pool.Ping(context.Background())

	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) || !database.IsTransient(err) {
		t.Fatalf("TLS refused at connect = %v (connect error: %v), want a transient *pgconn.ConnectError", err, errors.As(err, &connectErr))
	}
}

func answerEverySSLRequestWithNo(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = io.ReadFull(conn, make([]byte, 8))
		_, _ = conn.Write([]byte("N"))
		_ = conn.Close()
	}
}

func killSessionOf(ctx context.Context, t *testing.T, conn *pgxpool.Conn, killer *pgxpool.Pool) {
	t.Helper()
	var pid int
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("backend pid: %v", err)
	}
	if _, err := killer.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("terminate: %v", err)
	}
}

func TestIsTransient_RealPostgresFailures(t *testing.T) {
	ctx, pool := probePool(t)
	_, killer := probePool(t)

	cases := map[string]struct {
		run  func(t *testing.T) error
		want bool
	}{
		"query cut off by its deadline": {func(t *testing.T) error {
			short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			_, err := pool.Exec(short, "SELECT pg_sleep(10)")
			return err
		}, true},
		"query cancelled by the caller": {func(t *testing.T) error {
			cancellable, cancel := context.WithCancel(ctx)
			query := "SELECT pg_sleep(10) /* " + uuid.NewString() + " */"
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				defer cancel()
				timingtest.Within(10*time.Second, func() bool {
					if cancellable.Err() != nil {
						return true
					}
					var n int
					err := killer.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state = 'active' AND query = $1", query).Scan(&n)
					return err == nil && n > 0
				})
			}()
			defer func() {
				cancel()
				<-finished
			}()
			_, err := pool.Exec(cancellable, query)
			return err
		}, false},
		"query on an already cancelled context": {func(t *testing.T) error {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			_, err := pool.Exec(cancelled, "SELECT 1")
			return err
		}, false},
		"session terminated by the server": {func(t *testing.T) error {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()
			killSessionOf(ctx, t, conn, killer)
			_, err = conn.Exec(ctx, "SELECT 1")
			return err
		}, true},
		"statement on a session the server already closed": {func(t *testing.T) error {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer conn.Release()
			killSessionOf(ctx, t, conn, killer)
			_, _ = conn.Exec(ctx, "SELECT 1")
			_, err = conn.Exec(ctx, "SELECT 1")
			return err
		}, true},
		"second statement while rows are still open (conn busy)": {func(t *testing.T) error {
			return database.InTx(ctx, pool, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, "SELECT generate_series(1, 3)")
				if err != nil {
					t.Fatalf("query: %v", err)
				}
				defer rows.Close()
				_, err = tx.Exec(ctx, "SELECT 1")
				if rowsErr := rows.Err(); rowsErr != nil {
					t.Fatalf("rows: %v", rowsErr)
				}
				return err
			})
		}, false},
		"a bug in the query": {func(t *testing.T) error {
			_, err := pool.Exec(ctx, "SELECT 1/0")
			return err
		}, false},
		"no rows": {func(t *testing.T) error {
			var one int
			return pool.QueryRow(ctx, "SELECT 1 WHERE false").Scan(&one)
		}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.run(t)
			if err == nil {
				t.Fatal("expected the failure to return an error")
			}
			if got := database.IsTransient(err); got != tc.want {
				t.Fatalf("IsTransient(%v) = %v, want %v", err, got, tc.want)
			}
		})
	}
}
