package service

import "sync"

type pendingTracker struct {
	mu  sync.Mutex
	set map[string]struct{}
}

func newPendingTracker() *pendingTracker {
	return &pendingTracker{set: make(map[string]struct{})}
}

func (p *pendingTracker) track(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, alreadyTracked := p.set[key]
	p.set[key] = struct{}{}
	return alreadyTracked
}

func (p *pendingTracker) untrack(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.set, key)
}
