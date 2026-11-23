package runloop

import (
	"context"
	"sync"
)

type Tracker struct {
	wg sync.WaitGroup
}

func (r *Tracker) Track(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

func (r *Tracker) Launch(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	r.Track(func() {
		defer RecoverContext(ctx, "search.v2.background_panic", "label", label)
		fn(ctx)
	})
}

func (r *Tracker) Wait() { r.wg.Wait() }
