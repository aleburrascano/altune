package memory_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestTrackRepoFake_Contract(t *testing.T) {
	contract.TrackRepositoryContract(t, func(*testing.T) contract.TrackRepositoryUnderTest {
		return catalog.NewInMemoryTrackRepo()
	})
}
