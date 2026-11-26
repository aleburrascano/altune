package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"testing"
)

func TestTrackRepoFake_LibraryCapContract(t *testing.T) {
	contracttest.TrackRepositoryLibraryCapContract(t, func(_ *testing.T, libraryCap int) contracttest.TrackRepositoryUnderTest {
		return library.NewInMemoryTrackRepo(library.WithInMemoryLibraryCap(libraryCap))
	})
}

func TestTrackRepoFake_Contract(t *testing.T) {
	contracttest.TrackRepositoryContract(t, func(*testing.T) contracttest.TrackRepositoryUnderTest {
		return library.NewInMemoryTrackRepo()
	})
}
