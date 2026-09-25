package service

import "sync"

// principalGate bounds how many admission slots a single principal holds at
// once. It fair-shares the global queue: check-and-reserve is atomic under the
// mutex so concurrent Schedule calls for one principal cannot exceed the cap.
// A non-positive cap disables the gate (every admit succeeds).
type principalGate struct {
	cap  int
	mu   sync.Mutex
	held map[string]int
}

func newPrincipalGate(capacity int) *principalGate {
	return &principalGate{cap: capacity, held: make(map[string]int)}
}

// admit reserves a slot for id, returning false when id already holds its full
// share. Callers that admit must release exactly once when the job finishes.
func (g *principalGate) admit(id string) bool {
	if g.cap <= 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[id] >= g.cap {
		return false
	}
	g.held[id]++
	return true
}

// release returns a slot reserved by admit. It is a no-op when the gate is
// disabled, so it pairs safely with every admitted job.
func (g *principalGate) release(id string) {
	if g.cap <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held[id] <= 1 {
		delete(g.held, id)
		return
	}
	g.held[id]--
}
