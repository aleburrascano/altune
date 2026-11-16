package catalogtest_test

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/internal/core/ports"
	"testing"
)

func TestAudioStoreFake_Contract(t *testing.T) {
	catalogtest.AudioStoreContract(t, func(*testing.T) ports.AudioStore {
		return catalogtest.NewAudioStore()
	})
}
