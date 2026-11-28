package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"errors"
	"testing"
	"time"
)

type trackedCooldowns struct {
	*memory.CooldownStore
	clock *clocktest.Manual
}

func (trackedCooldowns) AddTrack(*testing.T) ids.TrackId { return ids.NewTrackId() }

func (c trackedCooldowns) PassCooldown(_ *testing.T, d time.Duration) { c.clock.Advance(d) }

func TestCooldownStore_KeepsTheCooldownContract(t *testing.T) {
	contracttest.CooldownContract(t, func(*testing.T) contracttest.CooldownUnderTest {
		manual := clocktest.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		return trackedCooldowns{CooldownStore: memory.MustNewCooldownStore(manual.Func()), clock: manual}
	})
}

func TestNewCooldownStore_RefusesAMissingClock(t *testing.T) {
	store, err := memory.NewCooldownStore(nil)

	if store != nil || !errors.Is(err, memory.ErrCooldownClockRequired) {
		t.Fatalf("got (%v, %v), want no store and ErrCooldownClockRequired", store, err)
	}
}
