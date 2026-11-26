package fingerprintstatus_test

import (
	"altune/go-api/internal/app/fingerprintstatus"
	"altune/go-api/internal/download"
	"context"
	"errors"
	"fmt"
	"testing"
)

type scriptedIdentifier struct {
	download.AudioIdentifier
	err error
}

func (s scriptedIdentifier) Identify(context.Context, string, float64) (download.RecordingMatch, error) {
	return download.RecordingMatch{}, s.err
}

func TestTracker_VerifiedStaysFalseUntilALookupSucceeds(t *testing.T) {
	failing := fingerprintstatus.NewTracker(scriptedIdentifier{err: errors.New("invalid api key")})
	working := fingerprintstatus.NewTracker(scriptedIdentifier{})

	_, _ = failing.Identify(context.Background(), "a.opus", 0)
	_, _ = working.Identify(context.Background(), "a.opus", 0)

	if failing.Verified() {
		t.Error("Verified() = true after only failed lookups, want false")
	}
	if !working.Verified() {
		t.Error("Verified() = false after a successful lookup, want true")
	}
}

type sequencedIdentifier struct {
	download.AudioIdentifier
	errs []error
	next int
}

func (s *sequencedIdentifier) Identify(context.Context, string, float64) (download.RecordingMatch, error) {
	err := s.errs[s.next]
	s.next++
	return download.RecordingMatch{}, err
}

func TestTracker_VerifiedFollowsTheLatestLookup(t *testing.T) {
	throttled := fmt.Errorf("acoustid lookup: %w", download.ErrIdentifyThrottled)
	tracker := fingerprintstatus.NewTracker(&sequencedIdentifier{errs: []error{
		nil, throttled, errors.New("invalid api key"), throttled, nil,
	}})

	steps := []struct {
		name string
		want bool
	}{
		{"success", true},
		{"throttled after success", true},
		{"failure after success", false},
		{"throttled after failure", false},
		{"success after failure", true},
	}
	for _, step := range steps {
		_, _ = tracker.Identify(context.Background(), "a.opus", 0)
		if got := tracker.Verified(); got != step.want {
			t.Errorf("Verified() after %s = %v, want %v", step.name, got, step.want)
		}
	}
}
