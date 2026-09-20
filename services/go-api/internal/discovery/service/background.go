package service

import (
	"context"
	"sync"
)

// backgroundRunner owns the lifecycle of the search pipeline's fire-and-forget
// work (telemetry emit, vocabulary ingest, identity-bridge persistence, the
// behavioral-score refresh loop). It replaces the WaitGroup plus launchBackground
// helper that used to sit inline on Service, so every background-emitting
// collaborator depends on this one primitive instead of the whole god object.
// wait drains every tracked goroutine for graceful shutdown and tests.
type backgroundRunner struct {
	wg sync.WaitGroup
}

// track runs fn on a tracked goroutine. The caller owns fn's context and panic
// policy; launch layers request-detachment and panic recovery on top for the
// common fire-and-forget case.
func (r *backgroundRunner) track(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

// launch detaches fn from request cancellation, tracks it, and recovers+logs any
// panic so async work can never crash the process. Recovery goes through
// RecoverGoroutine so a detached panic is reported exactly like every other
// contained one: Error, under the detached context so the correlation id
// survives, with the stack that names the failing frame (#2244).
func (r *backgroundRunner) launch(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	r.track(func() {
		defer RecoverGoroutine(ctx, "search.v2.background_panic", "label", label)
		fn(ctx)
	})
}

func (r *backgroundRunner) wait() { r.wg.Wait() }
