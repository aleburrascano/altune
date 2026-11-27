package panicguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

type PanicError struct {
	Value any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", e.Value)
}

func Recovered(err error) (value any, ok bool) {
	var pe *PanicError
	if errors.As(err, &pe) {
		return pe.Value, true
	}
	return nil, false
}

func recovered(f func()) (pe *PanicError) {
	defer func() {
		if rec := recover(); rec != nil {
			pe = &PanicError{Value: rec}
		}
	}()
	f()
	return nil
}

func Try(f func()) error {
	if pe := recovered(f); pe != nil {
		return pe
	}
	return nil
}

func Call[T any](f func() T) (T, error) {
	var out T
	if pe := recovered(func() { out = f() }); pe != nil {
		var zero T
		return zero, pe
	}
	return out, nil
}

func Contain(ctx context.Context, msg string, f func(), attrs ...any) {
	if pe := recovered(f); pe != nil {
		slog.ErrorContext(ctx, msg, append(slices.Clip(attrs), "panic", pe.Value)...)
	}
}

type Group struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	errs []error
}

func (g *Group) Go(f func()) {
	g.wg.Go(func() {
		if pe := recovered(f); pe != nil {
			g.mu.Lock()
			g.errs = append(g.errs, pe)
			g.mu.Unlock()
		}
	})
}

func (g *Group) Wait() error {
	g.wg.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	return errors.Join(g.errs...)
}

type Once struct {
	once  sync.Once
	group Group
}

func (o *Once) Go(f func()) {
	o.once.Do(func() { o.group.Go(f) })
}

func (o *Once) Contain(ctx context.Context, msg string, f func(), attrs ...any) {
	o.Go(func() { Contain(ctx, msg, f, attrs...) })
}

func (o *Once) Wait() error {
	return o.group.Wait()
}

func WaitWithin(budget time.Duration, wait func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait()
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
