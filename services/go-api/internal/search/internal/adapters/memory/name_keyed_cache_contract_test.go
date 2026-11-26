package memory_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/adapters/memory/memorytest"
	"testing"
)

func TestNameKeyedCache_KeepsTheNameKeyedCacheContract(t *testing.T) {
	memorytest.NameKeyedCacheContract(t, func(*testing.T) memorytest.NameKeyedCacheUnderTest[string] {
		return memory.NewNameKeyedCache[string]()
	}, "daft punk data", "newer data", func(a, b string) bool { return a == b })
}
