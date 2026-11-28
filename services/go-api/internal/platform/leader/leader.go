package leader

import (
	"altune/go-api/internal/platform/leader/internal/domain"
	"altune/go-api/internal/platform/leader/internal/ports"
	"altune/go-api/internal/platform/leader/internal/service"
	"context"
)

type (
	Counters  = domain.Counters
	Election  = service.Election
	LockStore = ports.LockStore
)

var NewElection = service.NewElection

func AdaptLocks[S ports.LockSession](acquire func(context.Context) (S, error)) LockStore {
	return service.AdaptLocks(acquire)
}
