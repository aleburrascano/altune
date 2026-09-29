package service

import (
	"context"
	"sync"
)

type backgroundRunner struct {
	wg sync.WaitGroup
}

func (r *backgroundRunner) track(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

func (r *backgroundRunner) launch(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	r.track(func() {
		defer RecoverGoroutine(ctx, "search.v2.background_panic", "label", label)
		fn(ctx)
	})
}

func (r *backgroundRunner) wait() { r.wg.Wait() }
