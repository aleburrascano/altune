package eval

import (
	"altune/go-api/internal/shared/fanout"
	"context"
	"sync/atomic"
)

func runParallel[T any](ctx context.Context, items []T, concurrency int, progress func(done, total int), work func(i int, item T)) {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(items)
	step := total / 20
	if step < 1 {
		step = 1
	}

	var done int32
	g := fanout.New("eval.item_panic", concurrency)
	for i, item := range items {
		i, item := i, item
		g.Go(ctx, func() {
			defer func() {
				n := int(atomic.AddInt32(&done, 1))
				if progress != nil && (n%step == 0 || n == total) {
					progress(n, total)
				}
			}()
			work(i, item)
		})
	}
	_ = g.Wait()
}
