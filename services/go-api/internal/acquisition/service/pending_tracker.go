package service

import "sync"

type pendingTracker struct {
	wg  *sync.WaitGroup
	mu  sync.Mutex
	set map[string]struct{}
}

func newPendingTracker(wg *sync.WaitGroup) *pendingTracker {
	return &pendingTracker{wg: wg, set: make(map[string]struct{})}
}

func (p *pendingTracker) track(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, alreadyTracked := p.set[key]
	if !alreadyTracked {
		p.set[key] = struct{}{}
		p.wg.Add(1)
	}
	return alreadyTracked
}

func (p *pendingTracker) untrack(key string) {
	p.mu.Lock()
	_, tracked := p.set[key]
	if tracked {
		delete(p.set, key)
	}
	p.mu.Unlock()
	if tracked {
		p.wg.Done()
	}
}

func (p *pendingTracker) sweep() {
	p.mu.Lock()
	pending := p.set
	p.set = make(map[string]struct{})
	p.mu.Unlock()
	for range pending {
		p.wg.Done()
	}
}
