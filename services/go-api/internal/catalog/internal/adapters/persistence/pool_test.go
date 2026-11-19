package persistence_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dbCallTimeout = 5 * time.Second

const dbCallTimeoutSlack = 500 * time.Millisecond

func poolAt(t *testing.T, addr string) *pgxpool.Pool {
	t.Helper()
	return poolWith(t, addr, "")
}

func poolWith(t *testing.T, addr, params string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), fmt.Sprintf("postgres://altune:secret@%s/altune?sslmode=disable%s", addr, params))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func getMissingTrack(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := catalog.NewPgxTrackRepository(pool).GetByID(ctx, catalog.NewTrackId(), shared.NewUserId(uuid.New()))
	return err
}

func cancelledAfter(d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(d, cancel)
	return ctx, cancel
}

func TestPgxRepos_EachFailureShapeIsClassifiedTransientOrNot(t *testing.T) {
	cases := []struct {
		name      string
		addr      func(t *testing.T) string
		ctx       func() (context.Context, context.CancelFunc)
		transient bool
	}{
		{"connection refused", refusedAddr, nil, true},
		{"caller deadline passes while the server hangs", hungAddr, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 200*time.Millisecond)
		}, true},
		{"caller cancels while the server hangs", hungAddr, func() (context.Context, context.CancelFunc) {
			return cancelledAfter(200 * time.Millisecond)
		}, false},
		{"caller deadline passes mid-query", silentAfterStartupAddr, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 200*time.Millisecond)
		}, true},
		{"caller cancels mid-query", silentAfterStartupAddr, func() (context.Context, context.CancelFunc) {
			return cancelledAfter(200 * time.Millisecond)
		}, false},
		{"unreadable reply to the startup", garbledStartupAddr, nil, true},
		{"connection reset mid-query", resettingAddr, nil, true},
		{"backend terminated mid-query (57P01)", terminatingAddr("57P01"), nil, true},
		{"connection dropped mid-query", terminatingAddr(""), nil, true},
		{"08006 connection_failure", rejectingAddr("08006"), nil, true},
		{"08001 sqlclient_unable_to_establish", rejectingAddr("08001"), nil, true},
		{"53300 too_many_connections", rejectingAddr("53300"), nil, true},
		{"57P01 admin_shutdown", rejectingAddr("57P01"), nil, true},
		{"57P03 cannot_connect_now", rejectingAddr("57P03"), nil, true},
		{"40001 serialization_failure", rejectingAddr("40001"), nil, true},
		{"40P01 deadlock_detected", rejectingAddr("40P01"), nil, true},
		{"23505 unique_violation", rejectingAddr("23505"), nil, false},
		{"42P01 undefined_table", rejectingAddr("42P01"), nil, false},
		{"28P01 invalid_password", rejectingAddr("28P01"), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.Background(), context.CancelFunc(func() {})
			if tc.ctx != nil {
				ctx, cancel = tc.ctx()
			}
			defer cancel()

			err := getMissingTrack(ctx, poolAt(t, tc.addr(t)))

			if err == nil {
				t.Fatal("GetByID succeeded against a failing database")
			}
			if got := errors.Is(err, catalog.ErrDBTransient); got != tc.transient {
				t.Fatalf("transient = %v, want %v (err: %v)", got, tc.transient, err)
			}
		})
	}
}

func TestPgxTrackRepo_ADeadlineWhileWaitingForAPooledConnectionIsTransient(t *testing.T) {
	pool := poolWith(t, silentAfterStartupAddr(t), "&pool_max_conns=1")
	held, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire the only connection: %v", err)
	}
	defer held.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = getMissingTrack(ctx, pool)

	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, catalog.ErrDBTransient) {
		t.Fatalf("err = %v, want a transient context.DeadlineExceeded", err)
	}
}

func TestPgxPlaylistRepo_AddTrack_ACancelInsideItsTransactionIsNotTransient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	userId := shared.NewUserId(uuid.New())
	playlistId := seedPlaylistForDB(ctx, t, testPool(t), userId)
	pool, trace := tracedPool(t)
	trace.afterNextQuery(cancel)

	err := catalog.NewPgxPlaylistRepository(pool).AddTrack(ctx, userId, playlistId, catalog.NewTrackId())

	if !errors.Is(err, context.Canceled) || errors.Is(err, catalog.ErrDBTransient) {
		t.Fatalf("err = %v, want context.Canceled, not transient", err)
	}
}

func TestPgxPlaylistRepo_GetWithTracks_NamesATransientFailureOnce(t *testing.T) {
	pool := poolAt(t, refusedAddr(t))

	_, _, err := catalog.NewPgxPlaylistRepository(pool).GetWithTracks(context.Background(), catalog.NewPlaylistId(), shared.NewUserId(uuid.New()))

	if !errors.Is(err, catalog.ErrDBTransient) {
		t.Fatalf("err = %v, want ErrDBTransient", err)
	}
	if n := strings.Count(err.Error(), catalog.ErrDBTransient.Error()); n != 1 {
		t.Errorf("err %q names ErrDBTransient %d times, want once", err, n)
	}
}

type countingDBCallMetrics struct{ timeouts atomic.Int64 }

func (m *countingDBCallMetrics) DBCallTimedOut() { m.timeouts.Add(1) }

func recordDBCallTimeouts(t *testing.T) *countingDBCallMetrics {
	t.Helper()
	metrics := &countingDBCallMetrics{}
	catalog.SetDBCallMetrics(metrics)
	t.Cleanup(func() { catalog.SetDBCallMetrics(nil) })
	return metrics
}

type unansweredCall struct {
	name string
	call func(ctx context.Context, pool *pgxpool.Pool) error
}

func unansweredCalls(t *testing.T) []unansweredCall {
	userId := shared.NewUserId(uuid.New())
	track := newTestTrackForDB(t, userId)
	playlist, err := catalog.NewPlaylist(userId, "stuck", time.Now())
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}
	return []unansweredCall{
		{"track.Query/ListForUser", func(ctx context.Context, pool *pgxpool.Pool) error {
			_, _, err := catalog.NewPgxTrackRepository(pool).ListForUser(ctx, userId, 10, 0)
			return err
		}},
		{"track.QueryRow/GetByID", func(ctx context.Context, pool *pgxpool.Pool) error {
			_, err := catalog.NewPgxTrackRepository(pool).GetByID(ctx, track.ID, userId)
			return err
		}},
		{"track.Exec/Update", func(ctx context.Context, pool *pgxpool.Pool) error {
			return catalog.NewPgxTrackRepository(pool).Update(ctx, track, track.Version)
		}},
		{"track.Begin/Add", func(ctx context.Context, pool *pgxpool.Pool) error {
			_, _, err := catalog.NewPgxTrackRepository(pool).Add(ctx, track)
			return err
		}},
		{"playlist.Exec/Create", func(ctx context.Context, pool *pgxpool.Pool) error {
			return catalog.NewPgxPlaylistRepository(pool).Create(ctx, playlist)
		}},
		{"playlist.Begin/AddTrack", func(ctx context.Context, pool *pgxpool.Pool) error {
			return catalog.NewPgxPlaylistRepository(pool).AddTrack(ctx, userId, playlist.ID, track.ID)
		}},
		{"featured.Begin/ReplaceFeaturedArtists", func(ctx context.Context, pool *pgxpool.Pool) error {
			return catalog.NewPgxCatalogTrackRepository(pool).ReplaceFeaturedArtists(ctx, track.ID, userId, nil)
		}},
		{"lens.Query/ListFilteredForUser", func(ctx context.Context, pool *pgxpool.Pool) error {
			_, _, err := catalog.NewPgxCatalogTrackRepository(pool).ListFilteredForUser(ctx, userId, catalog.LibraryQuery{Limit: 10})
			return err
		}},
	}
}

type callResult struct {
	err     error
	elapsed time.Duration
}

func runConcurrently(t *testing.T, calls []unansweredCall) []callResult {
	t.Helper()
	results := make([]callResult, len(calls))
	var wg sync.WaitGroup
	for i, tc := range calls {
		pool := poolAt(t, hungAddr(t))
		wg.Go(func() {
			start := time.Now()
			err := tc.call(context.Background(), pool)
			results[i] = callResult{err: err, elapsed: time.Since(start)}
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return results
	case <-time.After(dbCallTimeout + 4*dbCallTimeoutSlack):
		t.Fatalf("calls still running after %v: an unanswered call was not cut", dbCallTimeout+4*dbCallTimeoutSlack)
		return nil
	}
}

func TestPgxRepos_AnUnansweredCallIsCutAtTheDBCallTimeoutAndCountedOnce(t *testing.T) {
	metrics := recordDBCallTimeouts(t)
	calls := unansweredCalls(t)

	results := runConcurrently(t, calls)

	for i, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			assertCutAtTheDBCallTimeout(t, results[i].err, results[i].elapsed)
		})
	}
	if got := metrics.timeouts.Load(); got != int64(len(calls)) {
		t.Errorf("DB-call timeouts = %d, want one for each of the %d calls", got, len(calls))
	}
}

func assertCutAtTheDBCallTimeout(t *testing.T, err error, elapsed time.Duration) {
	t.Helper()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed < dbCallTimeout || elapsed > dbCallTimeout+dbCallTimeoutSlack {
		t.Errorf("call returned after %v, want the %v DB-call timeout", elapsed, dbCallTimeout)
	}
}

func TestDBCallMetrics_ACallThatEndsBeforeItsOwnDeadlineIsNotCounted(t *testing.T) {
	cases := []struct {
		name string
		addr func(t *testing.T) string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"the caller's deadline", hungAddr, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 200*time.Millisecond)
		}},
		{"a fast failure", refusedAddr, func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := recordDBCallTimeouts(t)
			ctx, cancel := tc.ctx()
			defer cancel()

			if err := getMissingTrack(ctx, poolAt(t, tc.addr(t))); err == nil {
				t.Fatal("GetByID succeeded against a failing database")
			}
			if got := metrics.timeouts.Load(); got != 0 {
				t.Errorf("DB-call timeouts = %d, want 0", got)
			}
		})
	}
}

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

func silentAfterStartupAddr(t *testing.T) string {
	t.Helper()
	return fakePGAddr(t, func(conn net.Conn) {
		be, err := completeStartup(conn)
		for err == nil {
			var msg pgproto3.FrontendMessage
			if msg, err = be.Receive(); err == nil {
				if _, quit := msg.(*pgproto3.Terminate); quit {
					return
				}
			}
		}
	})
}

func garbledStartupAddr(t *testing.T) string {
	t.Helper()
	return fakePGAddr(t, func(conn net.Conn) {
		if _, err := pgproto3.NewBackend(conn, conn).ReceiveStartupMessage(); err == nil {
			_, _ = conn.Write([]byte{'!', 0, 0, 0, 4})
		}
	})
}

func resettingAddr(t *testing.T) string {
	t.Helper()
	return fakePGAddr(t, func(conn net.Conn) {
		be, err := completeStartup(conn)
		if err != nil {
			return
		}
		if _, err := be.Receive(); err == nil {
			_ = conn.(*net.TCPConn).SetLinger(0)
		}
	})
}

func completeStartup(conn net.Conn) (*pgproto3.Backend, error) {
	be := pgproto3.NewBackend(conn, conn)
	if _, err := be.ReceiveStartupMessage(); err != nil {
		return nil, err
	}
	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return be, be.Flush()
}

func rejectingAddr(code string) func(t *testing.T) string {
	return func(t *testing.T) string { return fakePGAddr(t, rejectStartup(code)) }
}

func terminatingAddr(code string) func(t *testing.T) string {
	return func(t *testing.T) string { return fakePGAddr(t, terminateOnFirstQuery(code)) }
}

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
