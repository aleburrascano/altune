package memory_test

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/contracttest"
	"testing"
)

func TestTrackRepoFake_LibraryCapContract(t *testing.T) {
	contracttest.TrackRepositoryLibraryCapContract(t, func(_ *testing.T, libraryCap int) contracttest.TrackRepositoryUnderTest {
		return memory.NewTrackRepo(memory.WithLibraryCap(libraryCap))
	})
}

func TestTrackRepoFake_Contract(t *testing.T) {
	contracttest.TrackRepositoryContract(t, func(*testing.T) contracttest.TrackRepositoryUnderTest {
		return memory.NewTrackRepo()
	})
}
