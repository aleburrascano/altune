package service

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"
)

type panicOnceTracker struct{ calls int }

func (p *panicOnceTracker) Create(context.Context, *domain.Report) (ports.IssueRef, error) {
	p.calls++
	if p.calls == 1 {
		panic("tracker blew up mid-create")
	}
	return ports.IssueRef{Number: 9, URL: "https://github.com/o/r/issues/9"}, nil
}

type heldTracker struct {
	entered chan struct{}
	release chan struct{}
}

func (h *heldTracker) Create(context.Context, *domain.Report) (ports.IssueRef, error) {
	close(h.entered)
	<-h.release
	return ports.IssueRef{Number: 3, URL: "https://github.com/o/r/issues/3"}, nil
}

type submitResult struct {
	ref ports.IssueRef
	err error
}

func submitAsync(ctx context.Context, svc *SubmitReportService, user shared.UserId) <-chan submitResult {
	results := make(chan submitResult, 1)
	go func() {
		ref, err := svc.Execute(ctx, user, keyedInput())
		results <- submitResult{ref: ref, err: err}
	}()
	return results
}

func awaitWithin(t *testing.T, results <-chan submitResult) submitResult {
	t.Helper()
	select {
	case got := <-results:
		return got
	case <-time.After(time.Second):
		t.Fatal("keyed submission still blocked after 1s")
		return submitResult{}
	}
}

func submitRecoveringPanic(svc *SubmitReportService, user shared.UserId) (recovered any) {
	defer func() { recovered = recover() }()
	_, _ = svc.Execute(context.Background(), user, keyedInput())
	return nil
}

func TestSubmitReport_KeyedRetryAfterPanickedCreateCreatesInsteadOfHanging(t *testing.T) {
	svc, _ := throttledService(&panicOnceTracker{})
	user := newUser()
	if submitRecoveringPanic(svc, user) == nil {
		t.Fatal("first submission returned normally, want the tracker panic to propagate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got := awaitWithin(t, submitAsync(ctx, svc, user))

	if got.err != nil || got.ref.Number != 9 {
		t.Fatalf("retry = %+v, %v; want a fresh issue 9", got.ref, got.err)
	}
}

func TestSubmitReport_DuplicateWaiterWithCancelledContextReturnsWhileCreatorIsHeld(t *testing.T) {
	tracker := &heldTracker{entered: make(chan struct{}), release: make(chan struct{})}
	svc, _ := throttledService(tracker)
	user := newUser()
	creator := submitAsync(context.Background(), svc, user)
	<-tracker.entered
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	waiter := awaitWithin(t, submitAsync(cancelled, svc, user))
	close(tracker.release)

	if !errors.Is(waiter.err, context.Canceled) {
		t.Fatalf("waiter err = %v, want context.Canceled", waiter.err)
	}
	if first := awaitWithin(t, creator); first.err != nil || first.ref.Number != 3 {
		t.Fatalf("creator = %+v, %v; want issue 3 unaffected by the waiter", first.ref, first.err)
	}
}
