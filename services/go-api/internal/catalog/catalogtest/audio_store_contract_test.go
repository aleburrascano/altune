package catalogtest_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	"testing"
)

func TestAudioStoreFake_Contract(t *testing.T) {
	catalogtest.AudioStoreContract(t, func(*testing.T) catalog.AudioStore {
		return catalogtest.NewAudioStore()
	})
}
