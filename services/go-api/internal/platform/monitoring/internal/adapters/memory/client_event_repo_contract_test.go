package memory_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/memory"
	"altune/go-api/internal/platform/monitoring/internal/contracttest"
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/google/uuid"
)

type memoryClientEvents struct {
	*memory.ClientEventRepo
}

func (memoryClientEvents) NewUser(*testing.T) ids.UserId { return ids.NewUserId(uuid.New()) }

func (m memoryClientEvents) RegisterIdentity(_ *testing.T, user ids.UserId) {
	m.ClientEventRepo.RegisterIdentity(user)
}

func (m memoryClientEvents) Count(_ *testing.T, user ids.UserId) int {
	return m.ClientEventRepo.Count(user)
}

func TestClientEventRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.ClientEventRepoContract(t, func(*testing.T) contracttest.ClientEventRepoUnderTest {
		return memoryClientEvents{memory.NewClientEventRepo()}
	})
}
