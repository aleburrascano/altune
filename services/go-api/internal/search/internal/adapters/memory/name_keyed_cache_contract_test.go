package memory_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/contracttest"
	"testing"
)

func TestNameKeyedCache_KeepsTheNameKeyedCacheContract(t *testing.T) {
	contracttest.NameKeyedCacheContract(t, func(*testing.T) contracttest.NameKeyedCacheUnderTest[string] {
		return memory.NewNameKeyedCache[string]()
	}, "daft punk data", "newer data", func(a, b string) bool { return a == b })
}
