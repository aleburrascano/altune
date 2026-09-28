package reliability

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"testing"
	"time"
)

func TestPollSignalIndependentOfAdminRead(t *testing.T) {
	cases := []struct {
		name       string
		adminErr   error
		pollHealth goapi.Health
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
			_, _ = b.Collect(context.Background())
			b.poller.pollOnce(context.Background())

			if got := b.poller.currentStatus(); got != tc.wantReach {
				t.Errorf("poll status = %v, want %v (admin path must not influence it)", got, tc.wantReach)
			}
		})
	}
}

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

type panicChecker struct{}

func (panicChecker) Health(context.Context) (goapi.Health, error) {
	panic("reachability probe blew up")
}

func TestPollerContainsProbePanic(t *testing.T) {
	p := newReachPoller(panicChecker{}, defaultPollInterval)

	p.safePollOnce(context.Background())

	p = newReachPoller(panicChecker{}, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.run(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller.run did not return after panicking probes — panic escaped containment")
	}
}

func TestPollerStartsConnecting(t *testing.T) {
	p := newReachPoller(&fakeChecker{}, defaultPollInterval)
	if got := p.currentStatus(); got != goapi.StatusConnecting {
		t.Errorf("initial status = %v, want connecting", got)
	}
}

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
