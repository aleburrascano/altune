package storage_test

import (
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/contracttest"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/librarytest"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	contracttest.AudioStoreContract(t, func(t *testing.T) ports.AudioStore {
		return storage.NewFilesystemAudioStore(t.TempDir(), librarytest.DiscardFSCallMetrics())
	})
}
