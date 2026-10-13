package catalogtest_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestTrackRepoFake_Contract(t *testing.T) {
	catalogtest.TrackRepositoryContract(t, func(*testing.T) catalogtest.TrackRepositoryUnderTest {
		return catalogtest.NewTrackRepo()
	})
}
