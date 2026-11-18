package memory_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestAudioStoreFake_Contract(t *testing.T) {
	contract.AudioStoreContract(t, func(*testing.T) catalog.AudioStore {
		return catalog.NewInMemoryAudioStore()
	})
}
