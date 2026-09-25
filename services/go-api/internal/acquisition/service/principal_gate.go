package service

import "sync"

type principalGate struct {
	cap  int
	mu   sync.Mutex
	held map[string]int
}

func newPrincipalGate(capacity int) *principalGate {
	return &principalGate{cap: capacity, held: make(map[string]int)}
}

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
