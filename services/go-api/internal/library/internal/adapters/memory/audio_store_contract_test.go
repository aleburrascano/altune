package memory_test

import (
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/contracttest"
	"altune/go-api/internal/library/internal/ports"
	"testing"
)

func TestAudioStoreFake_Contract(t *testing.T) {
	contracttest.AudioStoreContract(t, func(*testing.T) ports.AudioStore {
		return memory.NewAudioStore()
	})
}
