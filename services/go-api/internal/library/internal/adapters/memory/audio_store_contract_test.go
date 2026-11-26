package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"testing"
)

func TestAudioStoreFake_Contract(t *testing.T) {
	contract.AudioStoreContract(t, func(*testing.T) library.AudioStore {
		return library.NewInMemoryAudioStore()
	})
}
