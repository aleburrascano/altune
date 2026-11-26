package background_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetach_OutlivesTheRequestAndKeepsItsCorrelationID(t *testing.T) {
	request, cancelRequest := context.WithCancel(logging.WithCorrelationID(context.Background(), "corr-detach-1"))
	detached, cancel := background.Detach(request, time.Minute)
	defer cancel()

	cancelRequest()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached ctx err = %v after the request ended, want nil", err)
	}
	if got := logging.CorrelationIDFromContext(detached); got != "corr-detach-1" {
		t.Errorf("corr id = %q, want the request's", got)
	}
}

func TestDetach_EndsAtItsOwnBudget(t *testing.T) {
	detached, cancel := background.Detach(context.Background(), 10*time.Millisecond)
	defer cancel()

	select {
	case <-detached.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("detached ctx did not end at its budget")
	}
	if !errors.Is(detached.Err(), context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", detached.Err())
	}
}

func TestDetach_DeadlineIsTheBudgetNotTheRequests(t *testing.T) {
	request, cancelRequest := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelRequest()
	before := time.Now()
	detached, cancel := background.Detach(request, time.Hour)
	defer cancel()

	deadline, ok := detached.Deadline()
	if !ok || deadline.Before(before.Add(59*time.Minute)) {
		t.Errorf("deadline = %v (set %v), want about an hour from now", deadline, ok)
	}
}

func TestDetachCancelable_KeepsTheRequestsValuesButNotItsCancellation(t *testing.T) {
	request, cancelRequest := context.WithCancel(logging.WithCorrelationID(context.Background(), "corr-detach-2"))
	detached, cancel := runloop.DetachCancelable(request)
	defer cancel()

	cancelRequest()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached ctx err = %v after the request ended, want nil", err)
	}
	if got := logging.CorrelationIDFromContext(detached); got != "corr-detach-2" {
		t.Errorf("corr id = %q, want the request's", got)
	}
}

func TestDetachCancelable_EndsWhenItsOwnCancelIsCalled(t *testing.T) {
	detached, cancel := runloop.DetachCancelable(context.Background())

	cancel()

	if !errors.Is(detached.Err(), context.Canceled) {
		t.Errorf("err = %v, want Canceled", detached.Err())
	}
}

func TestDetachCancelable_HasNoDeadline(t *testing.T) {
	request, cancelRequest := context.WithTimeout(context.Background(), time.Minute)
	defer cancelRequest()
	detached, cancel := runloop.DetachCancelable(request)
	defer cancel()

	if deadline, ok := detached.Deadline(); ok {
		t.Errorf("deadline = %v, want none", deadline)
	}
}

func TestWaitWithin_TrueWhenTheWaitEndsFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !background.WaitWithin(ctx, func() {}) {
		t.Fatal("WaitWithin = false for a wait that returned at once")
	}
}

func TestWaitWithin_FalseWhenTheDeadlineEndsFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	release := make(chan struct{})
	defer close(release)

	if background.WaitWithin(ctx, func() { <-release }) {
		t.Fatal("WaitWithin = true while the wait was still blocked")
	}
}
