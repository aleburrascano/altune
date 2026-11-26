package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"testing"
)

func TestOrphanedAudioFake_Contract(t *testing.T) {
	contract.OrphanedAudioQueueContract(t, func(*testing.T) (library.OrphanedAudioQueue, func(*library.Track)) {
		tracks := library.NewInMemoryTrackRepo()
		return library.NewInMemoryOrphanedAudioQueue(tracks), tracks.Seed
	})
}
