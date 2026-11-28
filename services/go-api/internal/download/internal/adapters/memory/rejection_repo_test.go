package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/contracttest"
	"testing"
	"time"
)

func TestRejectionRepo_KeepsTheRejectionRepoContract(t *testing.T) {
	contracttest.RejectionRepoContract(t, func(*testing.T) contracttest.RejectionRepoUnderTest {
		return memory.NewRejectionRepo(time.Now)
	})
}
