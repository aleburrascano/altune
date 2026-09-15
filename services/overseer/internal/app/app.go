// Package app is Overseer's composition root: it loads config, sets up logging,
// drives the registered buckets' collect cycle and serves the shell. It knows
// the registry and the shell, never a concrete bucket.
package app

import (
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownBudget = 15 * time.Second

// App holds the wired runtime.
type App struct {
	cfg      *config.Config
	registry *core.Registry
	server   *http.Server
}

// New wires the app from config against the process-wide bucket registry, which
// buckets have already self-registered into via their package init.
func New(cfg *config.Config) *App {
	handler := shell.NewHandler(core.Default)
	return &App{
		cfg:      cfg,
		registry: core.Default,
		server: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Handler:           handler.Router(cfg.OwnerToken),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Run serves until the process is signalled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	a.collectAll(ctx) // one synchronous pass so the first render has data
	go a.tickLoop(ctx)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("overseer listening", "addr", a.server.Addr, "config", a.cfg)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("overseer shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer shutdownCancel()
	return a.server.Shutdown(shutdownCtx)
}

// tickLoop runs the collect cycle on the configured interval until ctx is done.
func (a *App) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collectAll(ctx)
		}
	}
}

// collectAll drives Collect -> Store for every bucket. A bucket whose source is
// down logs and is skipped; it keeps serving its last-known state and the shell
// stays up (the outlives-the-app invariant in the small). A bucket that panics in
// Collect or Store is contained too: collectAll runs inside the tickLoop
// goroutine, where an unrecovered panic would crash the whole process, so the
// degrade-don't-crash invariant must hold here just as safeRender enforces it on
// the render side.
func (a *App) collectAll(ctx context.Context) {
	for _, b := range a.registry.Buckets() {
		signals, err := safeCollect(ctx, b)
		if err != nil {
			slog.WarnContext(ctx, "overseer.collect.failed", "bucket", b.Meta().ID, "error", err)
			continue
		}
		safeStore(ctx, b, signals)
	}
}

// safeCollect drives one bucket's Collect, converting a panic into an error so a
// single misbehaving bucket cannot crash the background collect loop. It mirrors
// shell.safeRender: the plugin contract promises containment for every bucket,
// present and future, not only on render.
func safeCollect(ctx context.Context, b core.Bucket) (signals []core.Signal, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "overseer.collect.panic", "bucket", b.Meta().ID, "recover", rec)
			signals, err = nil, fmt.Errorf("bucket %q panicked in Collect: %v", b.Meta().ID, rec)
		}
	}()
	return b.Collect(ctx)
}

// safeStore drives one bucket's Store, containing a panic for the same reason
// safeCollect does: it runs in the tickLoop goroutine.
func safeStore(ctx context.Context, b core.Bucket, signals []core.Signal) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "overseer.store.panic", "bucket", b.Meta().ID, "recover", rec)
		}
	}()
	b.Store(signals)
}
