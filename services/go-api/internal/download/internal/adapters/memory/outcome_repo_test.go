package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"testing"
	"time"
)

type recordedOutcomes struct{ *memory.OutcomeRepo }

func (r recordedOutcomes) Recorded(_ *testing.T, trackID string) []domain.AcquisitionOutcome {
	return r.OutcomeRepo.Recorded(trackID)
}

func TestOutcomeRepo_KeepsTheOutcomeRepoContract(t *testing.T) {
	contracttest.OutcomeRepoContract(t, func(*testing.T) contracttest.OutcomeRepoUnderTest {
		return recordedOutcomes{memory.NewOutcomeRepo(time.Now)}
	})
}
