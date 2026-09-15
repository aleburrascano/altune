package leader

import (
	"context"
	"sync"
)

// term is one leadership tenure. It tracks the cancel func of every context
// issued under it so that ending the term cancels them all synchronously,
// before the election goes on to unlock the advisory lock.
type term struct {
	mu      sync.Mutex
	ended   bool
	nextID  uint64
	cancels map[uint64]context.CancelCauseFunc
}

func newTerm() *term {
	return &term{cancels: make(map[uint64]context.CancelCauseFunc)}
}

// issue derives a context from parent that end() cancels with
// ErrLeadershipLost. ok is false once the term has ended.
func (t *term) issue(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	id := t.nextID
	t.nextID++
	t.cancels[id] = cancel
	return ctx, func() { t.forget(id); cancel(context.Canceled) }, true
}

func (t *term) forget(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cancels, id)
}

// end cancels every context still issued under the term and refuses new ones.
func (t *term) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = true
	for _, cancel := range t.cancels {
		cancel(ErrLeadershipLost)
	}
	t.cancels = nil
}
