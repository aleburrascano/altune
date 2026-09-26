package reliability

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"testing"
	"time"
)

// TestPollSignalIndependentOfAdminRead is the spine proof: the reachability poll
// derives up/down entirely from its own /health probes, sharing no state with
// the admin-read (mirror) path. It exercises every cross of the two paths — the
// crucial cases being admin-up/poll-down (the mirror can't mask a real outage)
// and admin-down/poll-up (a degraded admin API is not reported as an outage).
func TestPollSignalIndependentOfAdminRead(t *testing.T) {
	cases := []struct {
		name       string
		adminErr   error        // what the admin-read path reports
		pollHealth goapi.Health // what the own poll's /health returns
		pollErr    error
		wantReach  goapi.Status
	}{
		{"both up", nil, goapi.Health{Status: "ok"}, nil, goapi.StatusUp},
		{"app fully down (observe + poll down)", srcDown("GET /observe/health"), goapi.Health{}, srcDown("GET /health"), goapi.StatusDown},
		{"observe up but app unreachable — poll still down", nil, goapi.Health{}, srcDown("GET /health"), goapi.StatusDown},
		{"observe down but app reachable — poll stays up", srcDown("GET /observe/health"), goapi.Health{Status: "ok"}, nil, goapi.StatusUp},
		{"app reachable but degraded — poll stays up", nil, goapi.Health{Status: "degraded"}, nil, goapi.StatusUp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			reader.set(healthyHealth(), tc.adminErr)
			checker := &fakeChecker{}
			checker.set(tc.pollHealth, tc.pollErr)

			b := newBucket(reader, checker, defaultPollInterval)
			// Drive the admin read (mirror) and the poll; the poll's verdict must
			// depend only on its own probe.
			_, _ = b.Collect(context.Background())
			b.poller.pollOnce(context.Background())

			if got := b.poller.currentStatus(); got != tc.wantReach {
				t.Errorf("poll status = %v, want %v (admin path must not influence it)", got, tc.wantReach)
			}
		})
	}
}

// TestPollerBoundedSamples proves the poll's own outcome ring is bounded no
// matter how many probes run.
func TestPollerBoundedSamples(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "ok"}, nil)
	p := newReachPoller(checker, defaultPollInterval)

	for i := 0; i < 4*pollCapacity; i++ {
		p.pollOnce(context.Background())
	}
	if got := p.samples.Len(); got != pollCapacity {
		t.Errorf("retained %d poll samples, want capped at %d", got, pollCapacity)
	}
}

// panicChecker's Health panics, standing in for a misbehaving reachability probe.
type panicChecker struct{}

func (panicChecker) Health(context.Context) (goapi.Health, error) {
	panic("reachability probe blew up")
}

// TestPollerContainsProbePanic is the degrade-don't-crash proof for the poller's
// background goroutine: a probe that panics must be contained — the process
// survives (no re-panic escapes safePollOnce) and the detector keeps running so
// the next probe still executes. Without the recover in run(), this panic would
// crash the whole Overseer process, since the poll loop lives outside
// safeCollect's recover.
func TestPollerContainsProbePanic(t *testing.T) {
	p := newReachPoller(panicChecker{}, defaultPollInterval)

	// A direct panicking probe is contained, not propagated.
	p.safePollOnce(context.Background())

	// The detector still runs: start run() with a short interval, let it tick
	// through several panicking probes, then cancel — it must return cleanly
	// rather than having taken down the goroutine or the process.
	p = newReachPoller(panicChecker{}, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond) // several ticks, each panicking and recovered
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller.run did not return after panicking probes — panic escaped containment")
	}
}

// TestPollerStartsConnecting proves the poller reports neither up nor down until
// its first probe — the render shows "first poll pending" rather than a false up.
func TestPollerStartsConnecting(t *testing.T) {
	p := newReachPoller(&fakeChecker{}, defaultPollInterval)
	if got := p.currentStatus(); got != goapi.StatusConnecting {
		t.Errorf("initial status = %v, want connecting", got)
	}
}

// TestPollerRunExitsOnCancel proves the background goroutine drains on ctx
// cancellation — no goroutine leaks past the app's lifetime.
func TestPollerRunExitsOnCancel(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{}, errors.New("down"))
	p := newReachPoller(checker, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller.run did not return within 2s of ctx cancel — goroutine leak")
	}
	if got := p.currentStatus(); got != goapi.StatusDown {
		t.Errorf("after probing a down app, status = %v, want down", got)
	}
}
