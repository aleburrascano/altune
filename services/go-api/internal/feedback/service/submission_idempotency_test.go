package service

import (
	"context"
	"errors"
	"testing"
)

type mayExistErr struct{}

func (mayExistErr) Error() string     { return "github issues: timed out after the request was sent" }
func (mayExistErr) ErrorCode() string { return "tracker_outcome_unknown" }
func (mayExistErr) Uncreated() bool   { return false }

func keyedInput() SubmitReportInput {
	input := validInput()
	input.IdempotencyKey = keyPtr("retry-after-ambiguous-failure")
	return input
}

func TestSubmitReport_KeyedRetryAfterAmbiguousFailureReplaysWithoutCallingTracker(t *testing.T) {
	tracker := &countingTracker{err: mayExistErr{}}
	svc, _ := throttledService(tracker)
	user := newUser()
	_, _ = svc.Execute(context.Background(), user, keyedInput())
	tracker.err = nil

	_, err := svc.Execute(context.Background(), user, keyedInput())

	if !errors.As(err, new(mayExistErr)) || tracker.n != 1 {
		t.Fatalf("retry = %v after %d tracker calls, want the replayed ambiguous failure after 1", err, tracker.n)
	}
}

func TestSubmitReport_KeyedRetryAfterVouchedUncreatedFailureCallsTrackerAgain(t *testing.T) {
	tracker := &countingTracker{err: uncreatedErr{code: "tracker_unreachable"}}
	svc, _ := throttledService(tracker)
	user := newUser()
	_, _ = svc.Execute(context.Background(), user, keyedInput())
	tracker.err = nil

	_, err := svc.Execute(context.Background(), user, keyedInput())

	if err != nil || tracker.n != 2 {
		t.Fatalf("retry = %v after %d tracker calls, want a fresh create (2 calls)", err, tracker.n)
	}
}

func TestSubmitReport_AmbiguousFailureIsForgottenAfterTheKeyTTL(t *testing.T) {
	tracker := &countingTracker{err: mayExistErr{}}
	svc, clock := throttledService(tracker)
	user := newUser()
	_, _ = svc.Execute(context.Background(), user, keyedInput())
	tracker.err = nil
	clock.advance(idempotencyTTL)

	_, err := svc.Execute(context.Background(), user, keyedInput())

	if err != nil || tracker.n != 2 {
		t.Fatalf("retry past the TTL = %v after %d tracker calls, want a fresh create (2 calls)", err, tracker.n)
	}
}
