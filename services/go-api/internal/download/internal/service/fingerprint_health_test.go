package service_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"context"
	"errors"
	"fmt"
	"testing"
)

type scriptedIdentifier struct {
	ports.AudioIdentifier
	err error
}

func (s scriptedIdentifier) Identify(context.Context, string, float64) (domain.RecordingMatch, error) {
	return domain.RecordingMatch{}, s.err
}

func TestTracker_VerifiedStaysFalseUntilALookupSucceeds(t *testing.T) {
	failing := service.NewFingerprintHealth(scriptedIdentifier{err: errors.New("invalid api key")})
	working := service.NewFingerprintHealth(scriptedIdentifier{})

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
	ports.AudioIdentifier
	errs []error
	next int
}

func (s *sequencedIdentifier) Identify(context.Context, string, float64) (domain.RecordingMatch, error) {
	err := s.errs[s.next]
	s.next++
	return domain.RecordingMatch{}, err
}

func TestTracker_VerifiedFollowsTheLatestLookup(t *testing.T) {
	throttled := fmt.Errorf("acoustid lookup: %w", domain.ErrIdentifyThrottled)
	tracker := service.NewFingerprintHealth(&sequencedIdentifier{errs: []error{
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
