package catalogtest_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"testing"
)

func TestOrphanedAudioFake_Contract(t *testing.T) {
	catalogtest.OrphanedAudioQueueContract(t, func(*testing.T) (ports.OrphanedAudioQueue, func(*domain.Track)) {
		tracks := catalogtest.NewTrackRepo()
		return catalogtest.NewOrphanedAudioQueue(tracks), tracks.Seed
	})
}
