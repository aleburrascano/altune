package service

import (
	"context"
	"log/slog"
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
// panic so async work can never crash the process.
func (r *backgroundRunner) launch(parentCtx context.Context, label string, fn func(ctx context.Context)) {
	ctx := context.WithoutCancel(parentCtx)
	r.track(func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Warn("search.v2.background_panic", "label", label, "error", rec)
			}
		}()
		fn(ctx)
	})
}

func (r *backgroundRunner) wait() { r.wg.Wait() }
