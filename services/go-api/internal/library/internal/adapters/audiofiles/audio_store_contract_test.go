package audiofiles_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	contract.AudioStoreContract(t, func(t *testing.T) library.AudioStore {
		return library.NewFilesystemAudioStore(t.TempDir())
	})
}
