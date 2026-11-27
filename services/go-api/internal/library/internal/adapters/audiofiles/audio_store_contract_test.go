package audiofiles_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"altune/go-api/internal/library/librarytest"
	"testing"
)

func TestFilesystemAudioStore_Contract(t *testing.T) {
	contracttest.AudioStoreContract(t, func(t *testing.T) library.AudioStore {
		return library.NewFilesystemAudioStore(t.TempDir(), librarytest.DiscardFSCallMetrics())
	})
}
