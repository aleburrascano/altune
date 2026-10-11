package service

import (
	"context"
	"sync"
)

type resolveFlight struct {
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	result  rankedResolution
}

type sharedResolver struct {
	mu      sync.Mutex
	flights map[string]*resolveFlight
}

func newSharedResolver() *sharedResolver {
	return &sharedResolver{flights: map[string]*resolveFlight{}}
}

func (s *sharedResolver) resolve(ctx context.Context, key string, run func(context.Context) rankedResolution) rankedResolution {
	f := s.join(ctx, key, run)
	select {
	case <-f.done:
		s.leave(key, f)
		return f.result
	case <-ctx.Done():
		s.leave(key, f)
		return rankedResolution{partial: true}
	}
}

func (s *sharedResolver) join(ctx context.Context, key string, run func(context.Context) rankedResolution) *resolveFlight {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.flights[key]; ok {
		f.waiters++
		return f
	}
	flightCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	f := &resolveFlight{cancel: cancel, done: make(chan struct{}), waiters: 1}
	s.flights[key] = f
	go func() {
		defer close(f.done)
		defer cancel()
		f.result = run(flightCtx)
		s.forget(key, f)
	}()
	return f
}

func (s *sharedResolver) leave(key string, f *resolveFlight) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.waiters--
	if f.waiters > 0 {
		return
	}
	select {
	case <-f.done:
	default:
		f.cancel()
		s.forgetLocked(key, f)
	}
}

func (s *sharedResolver) forget(key string, f *resolveFlight) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgetLocked(key, f)
}

func (s *sharedResolver) forgetLocked(key string, f *resolveFlight) {
	if s.flights[key] == f {
		delete(s.flights, key)
	}
}
