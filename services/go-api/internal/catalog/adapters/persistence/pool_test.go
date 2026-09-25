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
	"sync/atomic"
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

type countingDBCallMetrics struct{ timeouts atomic.Int64 }

func (m *countingDBCallMetrics) DBCallTimedOut() { m.timeouts.Add(1) }

// TestWithDBTimeout_CountsOnlyItsOwnDeadline proves the DB-call timeout counter
// moves when the persistence per-call deadline cuts off a wedged call, and stays
// put when the caller gave up first or the call completed in time.
func TestWithDBTimeout_CountsOnlyItsOwnDeadline(t *testing.T) {
	restore := dbCallTimeout
	dbCallTimeout = 50 * time.Millisecond
	metrics := &countingDBCallMetrics{}
	SetDBCallMetrics(metrics)
	t.Cleanup(func() {
		dbCallTimeout = restore
		SetDBCallMetrics(nil)
	})

	repo := &PgxTrackRepository{pool: blockingPool{}}
	userID := shared.NewUserId(uuid.New())

	t.Run("wedged call cut off by the per-call deadline is counted", func(t *testing.T) {
		before := metrics.timeouts.Load()
		if _, err := repo.GetByID(context.Background(), domain.NewTrackId(), userID); err == nil {
			t.Fatal("want a deadline error from the wedged pool, got nil")
		}
		if got := metrics.timeouts.Load(); got != before+1 {
			t.Errorf("DB-call timeouts = %d, want %d", got, before+1)
		}
	})

	t.Run("caller deadline is not a DB-call timeout", func(t *testing.T) {
		before := metrics.timeouts.Load()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		if _, err := repo.GetByID(ctx, domain.NewTrackId(), userID); err == nil {
			t.Fatal("want a context error, got nil")
		}
		if got := metrics.timeouts.Load(); got != before {
			t.Errorf("DB-call timeouts = %d, want %d (caller deadline must not count)", got, before)
		}
	})

	t.Run("call finished within the deadline is not counted", func(t *testing.T) {
		before := metrics.timeouts.Load()
		_, cancel := withDBTimeout(context.Background())
		cancel()
		if got := metrics.timeouts.Load(); got != before {
			t.Errorf("DB-call timeouts = %d, want %d", got, before)
		}
	})

	t.Run("repeated cancel after a timeout counts once", func(t *testing.T) {
		before := metrics.timeouts.Load()
		ctx, cancel := withDBTimeout(context.Background())
		<-ctx.Done()
		cancel()
		cancel()
		if got := metrics.timeouts.Load(); got != before+1 {
			t.Errorf("DB-call timeouts = %d, want %d", got, before+1)
		}
	})
}

// blockingPool is a pgxPool whose every call blocks until its context is done,
// then returns that context's error. It stands in for a wedged database: a
// connection the client holds open while the server never answers. With the
// per-call deadline in place a repository call against it must return in ~the
// deadline; without the deadline it would block forever.
type blockingPool struct{}

func (blockingPool) Begin(ctx context.Context) (pgx.Tx, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingPool) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingPool) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	<-ctx.Done()
	return errRow{err: ctx.Err()}
}

func (blockingPool) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	<-ctx.Done()
	return pgconn.CommandTag{}, ctx.Err()
}

type errRow struct{ err error }

func (r errRow) Scan(_ ...any) error { return r.err }

// TestPersistenceAdapters_StuckCallIsBounded proves that a stuck dependency call
// in each catalog persistence adapter is bounded by the per-call deadline rather
// than blocking the handler goroutine (and its pooled connection) indefinitely.
// It exercises every pgxPool entry point (Begin, Query, QueryRow, Exec) across
// all four adapters against a pool that never answers.
func TestPersistenceAdapters_StuckCallIsBounded(t *testing.T) {
	restore := dbCallTimeout
	dbCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { dbCallTimeout = restore })

	pool := blockingPool{}
	trackRepo := &PgxTrackRepository{pool: pool}
	playlistRepo := &PgxPlaylistRepository{pool: pool}
	featuredRepo := &PgxFeaturedArtistRepository{pool: pool}
	lensRepo := &PgxLibraryLensRepository{pool: pool}

	userId := shared.NewUserId(uuid.New())
	track := newTestTrackForDB(t, userId)
	playlist, err := domain.NewPlaylist(userId, "stuck", time.Now())
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}

	cases := []struct {
		name string
		call func(context.Context) error
	}{
		{"track.Query/ListForUser", func(ctx context.Context) error {
			_, _, err := trackRepo.ListForUser(ctx, userId, 10, 0)
			return err
		}},
		{"track.QueryRow/GetByID", func(ctx context.Context) error {
			_, err := trackRepo.GetByID(ctx, track.ID, userId)
			return err
		}},
		{"track.Exec/Update", func(ctx context.Context) error {
			return trackRepo.Update(ctx, track, track.Version)
		}},
		{"track.Begin/Add", func(ctx context.Context) error {
			_, _, err := trackRepo.Add(ctx, track)
			return err
		}},
		{"playlist.Exec/Create", func(ctx context.Context) error {
			return playlistRepo.Create(ctx, playlist)
		}},
		{"playlist.Begin/AddTrack", func(ctx context.Context) error {
			return playlistRepo.AddTrack(ctx, userId, playlist.ID, track.ID)
		}},
		{"featured.Begin/ReplaceFeaturedArtists", func(ctx context.Context) error {
			return featuredRepo.ReplaceFeaturedArtists(ctx, track.ID, userId, nil)
		}},
		{"lens.Query/ListFilteredForUser", func(ctx context.Context) error {
			_, _, err := lensRepo.ListFilteredForUser(ctx, userId, domain.LibraryQuery{Limit: 10})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- tc.call(context.Background()) }()

			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("expected a deadline error, got nil")
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected context.DeadlineExceeded, got %v", err)
				}
				if elapsed := time.Since(start); elapsed > 2*time.Second {
					t.Fatalf("call returned after %v, far beyond the %v deadline", elapsed, dbCallTimeout)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("call did not return within 2s — the stuck dependency was not bounded")
			}
		})
	}
}
