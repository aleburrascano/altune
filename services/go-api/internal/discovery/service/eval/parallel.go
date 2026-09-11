package eval

import (
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

func runParallel[T any](items []T, concurrency int, progress func(done, total int), work func(i int, item T)) {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(items)
	step := total / 20
	if step < 1 {
		step = 1
	}

	var done int32
	g := new(errgroup.Group)
	g.SetLimit(concurrency)
	for i, item := range items {
		i, item := i, item
		g.Go(func() error {
			work(i, item)
			n := int(atomic.AddInt32(&done, 1))
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()
}
