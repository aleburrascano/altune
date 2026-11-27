package service

import (
	"sync"
	"sync/atomic"
)

type admission struct {
	closed    atomic.Bool
	paused    atomic.Bool
	stop      chan struct{}
	closeOnce sync.Once
	mu        sync.RWMutex
}

func newAdmission() *admission {
	return &admission{stop: make(chan struct{})}
}

func (a *admission) close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed.Store(true)
	a.closeOnce.Do(func() { close(a.stop) })
}
