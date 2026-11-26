package memory_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"testing"
)

func TestNameKeyedCache_KeepsTheNameKeyedCacheContract(t *testing.T) {
	memory.NameKeyedCacheContract(t, func(*testing.T) memory.NameKeyedCacheUnderTest[string] {
		return memory.NewNameKeyedCache[string]()
	}, "daft punk data", "newer data", func(a, b string) bool { return a == b })
}
