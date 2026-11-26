package background

import "context"

func Go(ctx context.Context, event string, fn func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = SafeRun(ctx, event, fn)
	}()
	return done
}
