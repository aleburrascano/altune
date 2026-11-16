package storage_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	catalogtest.AudioStoreContract(t, func(t *testing.T) catalog.AudioStore {
		return catalog.NewFilesystemAudioStore(t.TempDir())
	})
}
