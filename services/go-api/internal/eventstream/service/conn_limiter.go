package service

import (
	"altune/go-api/internal/eventstream/domain"
	"sync"
)

type ConnLimiter struct {
	mu        sync.Mutex
	maxPerKey int
	maxTotal  int
	total     int
	count     map[string]int
}

func NewConnLimiter(maxPerKey, maxTotal int) *ConnLimiter {
	return &ConnLimiter{maxPerKey: maxPerKey, maxTotal: maxTotal, count: make(map[string]int)}
}

func (l *ConnLimiter) Acquire(key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxPerKey > 0 && l.count[key] >= l.maxPerKey {
		return domain.ErrUserConnLimit
	}
	if l.maxTotal > 0 && l.total >= l.maxTotal {
		return domain.ErrGlobalConnLimit
	}
	l.count[key]++
	l.total++
	return nil
}

func (l *ConnLimiter) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, ok := l.count[key]
	if !ok {
		return
	}
	l.total--
	if n <= 1 {
		delete(l.count, key)
		return
	}
	l.count[key] = n - 1
}
