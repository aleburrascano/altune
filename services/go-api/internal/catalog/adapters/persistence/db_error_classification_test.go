package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestClassifyDBError pins which failures are flagged ports.ErrDBTransient.
// Server errors are real *pgconn.PgError values judged by SQLSTATE.
func TestClassifyDBError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		transient bool
	}{
		{"deadline exceeded", fmt.Errorf("scan: %w", context.DeadlineExceeded), true},
		{"caller cancelled", context.Canceled, false},
		{"unexpected EOF", fmt.Errorf("receive: %w", io.ErrUnexpectedEOF), true},
		{"08006 connection_failure", &pgconn.PgError{Code: "08006"}, true},
		{"08001 sqlclient_unable_to_establish", &pgconn.PgError{Code: "08001"}, true},
		{"53300 too_many_connections", &pgconn.PgError{Code: "53300"}, true},
		{"57P01 admin_shutdown", &pgconn.PgError{Code: "57P01"}, true},
		{"57P03 cannot_connect_now", &pgconn.PgError{Code: "57P03"}, true},
		{"40001 serialization_failure", &pgconn.PgError{Code: "40001"}, true},
		{"40P01 deadlock_detected", &pgconn.PgError{Code: "40P01"}, true},
		{"23505 unique_violation", &pgconn.PgError{Code: "23505"}, false},
		{"42P01 undefined_table", &pgconn.PgError{Code: "42P01"}, false},
		{"28P01 invalid_password", &pgconn.PgError{Code: "28P01"}, false},
		{"no rows", pgx.ErrNoRows, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDBError(tc.err)
			if gotTransient := errors.Is(got, ports.ErrDBTransient); gotTransient != tc.transient {
				t.Fatalf("transient = %v, want %v (err %v)", gotTransient, tc.transient, got)
			}
			if !errors.Is(got, tc.err) {
				t.Errorf("classified error %v lost the original %v", got, tc.err)
			}
		})
	}
	if classifyDBError(nil) != nil {
		t.Error("classifyDBError(nil) must stay nil")
	}
	once := classifyDBError(context.DeadlineExceeded)
	if twice := classifyDBError(once); twice.Error() != once.Error() {
		t.Errorf("re-classifying must be a no-op, got %v", twice)
	}
}

// TestTrackGetByID_ClassifiesRealConnectionFailures drives GetByID through a
// real pgxpool at local listeners that fail the way a sick database does, and
// asserts the error the hot path receives is (or is not) flagged transient.
func TestTrackGetByID_ClassifiesRealConnectionFailures(t *testing.T) {
	restore := dbCallTimeout
	dbCallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { dbCallTimeout = restore })

	cases := []struct {
		name      string
		addr      func(t *testing.T) string
		transient bool
	}{
		{"connection refused", refusedAddr, true},
		{"server accepts but never answers", hungAddr, true},
		{"server not accepting connections (57P03)", func(t *testing.T) string {
			return fakePGAddr(t, rejectStartup("57P03"))
		}, true},
		{"backend terminated mid-query (57P01)", func(t *testing.T) string {
			return fakePGAddr(t, terminateOnFirstQuery("57P01"))
		}, true},
		{"connection dropped mid-query", func(t *testing.T) string {
			return fakePGAddr(t, terminateOnFirstQuery(""))
		}, true},
		{"authentication rejected (28P01)", func(t *testing.T) string {
			return fakePGAddr(t, rejectStartup("28P01"))
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := tc.addr(t)
			pool, err := pgxpool.New(context.Background(),
				fmt.Sprintf("postgres://altune:secret@%s/altune?sslmode=disable", addr))
			if err != nil {
				t.Fatalf("pgxpool.New: %v", err)
			}
			t.Cleanup(pool.Close)

			repo := NewPgxTrackRepository(pool)
			_, err = repo.GetByID(context.Background(), domain.NewTrackId(), shared.NewUserId(uuid.New()))
			if err == nil {
				t.Fatal("GetByID succeeded against a failing database")
			}
			if got := errors.Is(err, ports.ErrDBTransient); got != tc.transient {
				t.Fatalf("transient = %v, want %v (err: %v)", got, tc.transient, err)
			}
		})
	}
}

// refusedAddr returns a local address nothing listens on.
func refusedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// hungAddr returns a local address that accepts TCP connections and never
// writes a byte: a wedged database the per-call deadline must cut off.
func hungAddr(t *testing.T) string {
	t.Helper()
	return fakePGAddr(t, func(conn net.Conn) {
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	})
}

// fakePGAddr serves every accepted connection with handle, then closes it.
func fakePGAddr(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				handle(conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// rejectStartup answers the startup message with a FATAL ErrorResponse.
func rejectStartup(code string) func(net.Conn) {
	return func(conn net.Conn) {
		be := pgproto3.NewBackend(conn, conn)
		if _, err := be.ReceiveStartupMessage(); err != nil {
			return
		}
		be.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: code, Message: "rejected by fake server"})
		_ = be.Flush()
	}
}

// terminateOnFirstQuery completes startup, then on the first query message
// sends a FATAL ErrorResponse with code (none when code is empty) and drops
// the connection, as pg_terminate_backend or a crashed backend would.
func terminateOnFirstQuery(code string) func(net.Conn) {
	return func(conn net.Conn) {
		be := pgproto3.NewBackend(conn, conn)
		if _, err := be.ReceiveStartupMessage(); err != nil {
			return
		}
		be.Send(&pgproto3.AuthenticationOk{})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if err := be.Flush(); err != nil {
			return
		}
		if _, err := be.Receive(); err != nil {
			return
		}
		if code != "" {
			be.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: code, Message: "terminating connection due to administrator command"})
			_ = be.Flush()
		}
	}
}
