package storage_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	contract.AudioStoreContract(t, func(t *testing.T) catalog.AudioStore {
		return catalog.NewFilesystemAudioStore(t.TempDir())
	})
}
