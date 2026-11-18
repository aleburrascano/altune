package memory_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestOrphanedAudioFake_Contract(t *testing.T) {
	contract.OrphanedAudioQueueContract(t, func(*testing.T) (catalog.OrphanedAudioQueue, func(*catalog.Track)) {
		tracks := catalog.NewInMemoryTrackRepo()
		return catalog.NewInMemoryOrphanedAudioQueue(tracks), tracks.Seed
	})
}
