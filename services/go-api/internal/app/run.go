package app

import (
	"altune/go-api/internal/app/providertransport"
	"altune/go-api/internal/app/server"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	sharedRedis "altune/go-api/internal/shared/redis"
)

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := database.NewPool(ctx, a.cfg.DatabaseURL, a.cfg.DBPoolMaxConns)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	redisClient := sharedRedis.NewClient(ctx, a.cfg.RedisURL, a.cfg.RedisPoolSize)
	tracker, err := NewTracker(a.cfg)
	if err != nil {
		return err
	}
	transport, err := providertransport.Live()
	if err != nil {
		return err
	}
	assembly, err := Assemble(ctx, a.cfg, Handles{Pool: pool, Redis: redisClient, Transport: transport, Tracker: tracker, LogRing: a.logRing})
	if err != nil {
		return err
	}
	serveErr := serve(ctx, server.New(fmt.Sprintf("%s:%d", a.cfg.Host, a.cfg.Port), assembly.Handler()))
	cancel()
	slog.InfoContext(ctx, "waiting for background tasks")
	shutdownCtx, endShutdown := background.DetachCancelable(ctx)
	defer endShutdown()
	outcomes := assembly.Shutdown(shutdownCtx)
	shutdown.CloseDatastores(ctx, outcomes, shutdown.Datastores{Pool: pool, Redis: redisClient})
	slog.InfoContext(ctx, "shutdown complete")
	return serveErr
}

var errServerStopped = errors.New("http server goroutine ended before shutdown")

func serve(ctx context.Context, httpServer *http.Server) error {
	errCh := make(chan error, 1)
	listenDone := background.Go(ctx, "app.server_panic", func() {
		slog.InfoContext(ctx, "server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	})

	select {
	case err := <-errCh:
		return err
	case <-listenDone:
		select {
		case err := <-errCh:
			return err
		default:
			return errServerStopped
		}
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down")
	}

	drainCtx, endDrain := background.DetachCancelable(ctx)
	defer endDrain()
	shutdown.DrainServer(drainCtx, httpServer, shutdown.ServerDrainTimeout)
	return nil
}
