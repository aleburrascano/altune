package acquisitiontest_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/shared/ids"
	"testing"
)

type trackedCooldowns struct{ *acquisitiontest.CooldownStore }

func (trackedCooldowns) AddTrack(*testing.T) ids.TrackId { return ids.NewTrackId() }

func TestCooldownStore_KeepsTheCooldownContract(t *testing.T) {
	acquisitiontest.CooldownContract(t, func(*testing.T) acquisitiontest.CooldownUnderTest {
		return trackedCooldowns{acquisitiontest.NewCooldownStore()}
	})
}
