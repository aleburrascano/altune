package leader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// errConnector never hands out a connection, standing in for an exhausted pool,
// a rejected password, or a Postgres that is simply down.
type errConnector struct{ err error }

func (c errConnector) Acquire(context.Context) (heldConn, error) { return nil, c.err }

// refusingConn hands back no advisory lock. With lockErr nil the lock is merely
// held by another instance — the benign outcome of every standby's tick; with
// lockErr set the lock query itself failed.
type refusingConn struct {
	lockErr  error
	released bool
}

func (c *refusingConn) TryAdvisoryLock(context.Context, int64) (bool, error) {
	return false, c.lockErr
}

func (c *refusingConn) Ping(context.Context) error                  { return nil }
func (c *refusingConn) AdvisoryUnlock(context.Context, int64) error { return nil }
func (c *refusingConn) Release()                                    { c.released = true }

// captureWarnings redirects the default logger for one test and returns what a
// production operator would see at warn level and above.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(restore) })
	return &buf
}

func electionOver(db connector) *Election {
	return &Election{db: db, key: testKey, interval: 20 * time.Millisecond, won: make(chan struct{})}
}

// TestElection_PoolFailureIsLoggedAndCounted is the #1604 regression: a pool
// that cannot hand out a connection left acquire() silent and uncounted, so an
// instance wedged out of the election looked exactly like a healthy standby.
func TestElection_PoolFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)
	e := electionOver(errConnector{errors.New("pool exhausted")})

	e.acquire(context.Background())

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed pool acquire emitted no warning, got: %q", logged.String())
	}
	if !strings.Contains(logged.String(), "pool exhausted") {
		t.Errorf("the acquire warning did not carry the cause, got: %q", logged.String())
	}
	if c := e.Counters(); c.Attempts != 1 || c.Failures != 1 || c.Contended != 0 {
		t.Errorf("counters = %+v, want one attempt counted as a failure, not as contention", c)
	}
}

// TestElection_LockQueryFailureIsLoggedAndCounted covers the second half of the
// same class: the pool served a connection but the advisory-lock query itself
// failed, which is as much a stuck instance as never getting a connection.
func TestElection_LockQueryFailureIsLoggedAndCounted(t *testing.T) {
	logged := captureWarnings(t)
	conn := &refusingConn{lockErr: errors.New("connection reset by peer")}
	e := electionOver(fixedConnector{conn})

	e.acquire(context.Background())

	if !strings.Contains(logged.String(), "leader.acquire_failed") {
		t.Errorf("a failed lock query emitted no warning, got: %q", logged.String())
	}
	if c := e.Counters(); c.Failures != 1 || c.Contended != 0 {
		t.Errorf("counters = %+v, want the failed lock query counted as a failure", c)
	}
	if !conn.released {
		t.Error("the connection was not returned to the pool after the lock query failed")
	}
}

// TestElection_LostRaceIsCountedWithoutWarning is the other arm: losing the lock
// to a live leader is how a standby is supposed to spend every tick, so it must
// stay out of the warning stream that now carries real failures.
func TestElection_LostRaceIsCountedWithoutWarning(t *testing.T) {
	logged := captureWarnings(t)
	conn := &refusingConn{}
	e := electionOver(fixedConnector{conn})

	e.acquire(context.Background())

	if logged.String() != "" {
		t.Errorf("losing the race to another instance warned, got: %q", logged.String())
	}
	if c := e.Counters(); c.Contended != 1 || c.Failures != 0 {
		t.Errorf("counters = %+v, want the lost race counted as contention, not as a failure", c)
	}
	if !conn.released {
		t.Error("the connection was not returned to the pool after losing the race")
	}
}

// TestElection_CountsWinsAndTermEnds proves the flapping signal: an instance
// that wins the lock and then loses its session leaves both halves of the round
// trip visible, which a live-only boolean erases the moment it flips back.
func TestElection_CountsWinsAndTermEnds(t *testing.T) {
	conn := &flakyConn{}
	e := electionOver(fixedConnector{conn})
	settleIntoLeadership(e)

	conn.dead = true
	e.tick(context.Background())

	if c := e.Counters(); c.Wins != 1 || c.TermEnds != 1 {
		t.Errorf("counters = %+v, want one win followed by one term end", c)
	}
}
