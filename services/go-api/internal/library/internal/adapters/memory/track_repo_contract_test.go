package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"testing"
)

func TestTrackRepoFake_Contract(t *testing.T) {
	contract.TrackRepositoryContract(t, func(*testing.T) contract.TrackRepositoryUnderTest {
		return library.NewInMemoryTrackRepo()
	})
}
