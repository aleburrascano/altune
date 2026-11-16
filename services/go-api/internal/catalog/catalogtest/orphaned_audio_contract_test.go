package catalogtest_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestOrphanedAudioFake_Contract(t *testing.T) {
	catalogtest.OrphanedAudioQueueContract(t, func(*testing.T) (catalog.OrphanedAudioQueue, func(*catalog.Track)) {
		tracks := catalogtest.NewTrackRepo()
		return catalogtest.NewOrphanedAudioQueue(tracks), tracks.Seed
	})
}
