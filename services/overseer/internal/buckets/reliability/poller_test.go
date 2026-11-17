package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollSignalIndependentOfAdminRead(t *testing.T) {
	cases := []struct {
		name       string
		adminErr   error
		pollHealth goapi.Health
		pollErr    error
		wantReach  string
	}{
		{"both up", nil, goapi.Health{Status: "ok"}, nil, "up"},
		{"app fully down (observe + poll down)", srcDown("GET /observe/health"), goapi.Health{}, srcDown("GET /health"), "down"},
		{"observe up but app unreachable — poll still down", nil, goapi.Health{}, srcDown("GET /health"), "down"},
		{"observe down but app reachable — poll stays up", srcDown("GET /observe/health"), goapi.Health{Status: "ok"}, nil, "up"},
		{"app reachable but degraded — poll stays up", nil, goapi.Health{Status: "degraded"}, nil, goapi.ReasonDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			reader.set(healthyHealth(), tc.adminErr)

			b, probe := probedBucket(t, reader)
			_, _ = b.Collect(context.Background())
			probe.answer(tc.pollHealth, tc.pollErr)

			if got := snapData(t, b.Snapshot()).Reachability; got != tc.wantReach {
				t.Errorf("reachability = %q, want %q (admin path must not influence it)", got, tc.wantReach)
			}
		})
	}
}

func TestPollerBoundedSamples(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	for i := 0; i < 4*pollCapacity; i++ {
		probe.answer(up())
	}
	if got := len(snapData(t, b.Snapshot()).Poll); got != pollCapacity {
		t.Errorf("retained %d poll samples, want capped at %d", got, pollCapacity)
	}
}

type panicChecker struct{ calls atomic.Int64 }

func (p *panicChecker) Health(context.Context) (goapi.Health, error) {
	p.calls.Add(1)
	panic("reachability probe blew up")
}

func TestPollerContainsProbePanic(t *testing.T) {
	checker := &panicChecker{}
	b := reliability.New(reliability.Deps{Checker: checker, Interval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())

	b.Start(ctx)
	eventually(t, "probes after the first panic", func() bool { return checker.calls.Load() >= 3 })
	cancel()

	joined := make(chan struct{})
	go func() {
		b.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("the poller did not stop after panicking probes — panic escaped containment")
	}
}

func TestPollerStartsConnecting(t *testing.T) {
	if got := snapData(t, newBucket(&fakeReader{}).Snapshot()).Reachability; got != "connecting" {
		t.Errorf("initial reachability = %q, want connecting", got)
	}
}

func TestPollerRunExitsOnCancel(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{}, errors.New("down"))
	b := reliability.New(reliability.Deps{Checker: checker, Interval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())

	b.Start(ctx)
	eventually(t, "a probe of the down app", func() bool { return checker.probes() > 0 })
	cancel()

	joined := make(chan struct{})
	go func() {
		b.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("the poller did not stop within 2s of ctx cancel — goroutine leak")
	}
	if got := snapData(t, b.Snapshot()).Reachability; got != "down" {
		t.Errorf("after probing a down app, reachability = %q, want down", got)
	}
}
