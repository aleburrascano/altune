package app

import (
	"altune/go-api/internal/app/providerhttp"
	"altune/go-api/internal/app/shutdown"
	"altune/go-api/internal/shared/database"
	"context"
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
	assembly, err := Assemble(ctx, a.cfg, Handles{Pool: pool, Redis: redisClient, Transport: providerhttp.Live(), LogRing: a.logRing})
	if err != nil {
		return err
	}
	serveErr := serve(ctx, a.newServer(ctx, assembly.Handler()))
	cancel()
	slog.InfoContext(ctx, "waiting for background tasks")
	outcomes := assembly.Shutdown()
	shutdown.CloseDatastores(ctx, outcomes, shutdown.Datastores{Pool: pool, Redis: redisClient})
	slog.InfoContext(ctx, "shutdown complete")
	return serveErr
}

func serve(ctx context.Context, server *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down")
	}

	shutdown.DrainServer(server, shutdown.ServerDrainTimeout)
	return nil
}
