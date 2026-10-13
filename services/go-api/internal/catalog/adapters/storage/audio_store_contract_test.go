package storage

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/ports"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	catalogtest.AudioStoreContract(t, func(t *testing.T) ports.AudioStore {
		return NewFilesystemAudioStore(t.TempDir())
	})
}
