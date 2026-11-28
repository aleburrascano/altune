package contracttest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"
	"time"
)

type CooldownUnderTest interface {
	ports.CooldownStore
	AddTrack(t *testing.T) ids.TrackId
	PassCooldown(t *testing.T, d time.Duration)
}

const (
	contractCooldown = time.Minute
	shortCooldown    = 10 * time.Millisecond
	pastShortWindow  = 25 * time.Millisecond
)

var neverReservedAt = time.Unix(1_700_000_000, 0).UTC()

func reserve(t *testing.T, s CooldownUnderTest, id ids.TrackId, kind domain.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration) {
	t.Helper()
	at, ok, wait, err := s.Reserve(context.Background(), id, kind, cooldown)
	if err != nil {
		t.Fatalf("Reserve %s: %v", kind, err)
	}
	return at, ok, wait
}

func wantAdmitted(t *testing.T, s CooldownUnderTest, id ids.TrackId, kind domain.CooldownKind, cooldown time.Duration, when string) time.Time {
	t.Helper()
	at, ok, wait := reserve(t, s, id, kind, cooldown)
	if !ok || at.IsZero() || wait != 0 {
		t.Fatalf("Reserve %s = (%v, %v, %v), want admitted with a time and no wait", when, at, ok, wait)
	}
	return at
}

func wantRefused(t *testing.T, s CooldownUnderTest, id ids.TrackId, kind domain.CooldownKind, cooldown time.Duration, when string) {
	t.Helper()
	at, ok, wait := reserve(t, s, id, kind, cooldown)
	if ok || !at.IsZero() || wait <= 0 || wait > cooldown {
		t.Errorf("Reserve %s = (%v, %v, %v), want refused with 0 < wait <= %v", when, at, ok, wait, cooldown)
	}
}

func release(t *testing.T, s CooldownUnderTest, id ids.TrackId, kind domain.CooldownKind, at time.Time) {
	t.Helper()
	if err := s.Release(context.Background(), id, kind, at); err != nil {
		t.Fatalf("Release %s: %v", kind, err)
	}
}

func contractAReservationInsideTheCooldownIsRefusedWithTheWait(t *testing.T, s CooldownUnderTest) {
	id := s.AddTrack(t)
	wantAdmitted(t, s, id, domain.CooldownRetry, contractCooldown, "the first time")
	wantRefused(t, s, id, domain.CooldownRetry, contractCooldown, "inside the cooldown")
}

func contractKindsAndTracksHaveTheirOwnCooldowns(t *testing.T, s CooldownUnderTest) {
	id, other := s.AddTrack(t), s.AddTrack(t)
	wantAdmitted(t, s, id, domain.CooldownRetry, contractCooldown, "retry")
	wantAdmitted(t, s, id, domain.CooldownReacquire, contractCooldown, "reacquire after a retry on the same track")
	wantAdmitted(t, s, other, domain.CooldownRetry, contractCooldown, "retry on another track")
}

func contractTheCooldownEndsAndAdmitsAgainWithALaterTime(t *testing.T, s CooldownUnderTest) {
	id := s.AddTrack(t)
	first := wantAdmitted(t, s, id, domain.CooldownRetry, shortCooldown, "the first time")
	s.PassCooldown(t, pastShortWindow)
	second := wantAdmitted(t, s, id, domain.CooldownRetry, shortCooldown, "after the cooldown")
	if !second.After(first) {
		t.Errorf("second admission at %v, want it after the first at %v", second, first)
	}
}

func contractReleasingTheCurrentReservationFreesTheSlot(t *testing.T, s CooldownUnderTest) {
	id := s.AddTrack(t)
	at := wantAdmitted(t, s, id, domain.CooldownRetry, contractCooldown, "the first time")
	release(t, s, id, domain.CooldownRetry, at)
	wantAdmitted(t, s, id, domain.CooldownRetry, contractCooldown, "after the release")
}

func contractReleasingAStaleReservationLeavesTheNewerOne(t *testing.T, s CooldownUnderTest) {
	id := s.AddTrack(t)
	stale := wantAdmitted(t, s, id, domain.CooldownRetry, shortCooldown, "the first time")
	s.PassCooldown(t, pastShortWindow)
	wantAdmitted(t, s, id, domain.CooldownRetry, shortCooldown, "after the cooldown")
	release(t, s, id, domain.CooldownRetry, stale)
	wantRefused(t, s, id, domain.CooldownRetry, contractCooldown, "after releasing the stale one")
}

func contractReleasingWhatWasNeverReservedIsNotAnError(t *testing.T, s CooldownUnderTest) {
	release(t, s, s.AddTrack(t), domain.CooldownRetry, neverReservedAt)
}

var cooldownCases = []struct {
	name string
	run  func(t *testing.T, s CooldownUnderTest)
}{
	{"a reservation inside the cooldown is refused with the wait", contractAReservationInsideTheCooldownIsRefusedWithTheWait},
	{"kinds and tracks have their own cooldowns", contractKindsAndTracksHaveTheirOwnCooldowns},
	{"the cooldown ends and admits again with a later time", contractTheCooldownEndsAndAdmitsAgainWithALaterTime},
	{"releasing the current reservation frees the slot", contractReleasingTheCurrentReservationFreesTheSlot},
	{"releasing a stale reservation leaves the newer one", contractReleasingAStaleReservationLeavesTheNewerOne},
	{"releasing what was never reserved is not an error", contractReleasingWhatWasNeverReservedIsNotAnError},
}

func CooldownContract(t *testing.T, newStore func(t *testing.T) CooldownUnderTest) {
	for _, c := range cooldownCases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newStore(t)) })
	}
}
