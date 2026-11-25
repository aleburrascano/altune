package app

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/history"
	"altune/overseer/internal/panicguard"
	"altune/overseer/internal/shell"
	"altune/overseer/internal/webui"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	defaultShutdownBudget = 15 * time.Second
	missedTicks           = 3
)

type App struct {
	cfg      *config.Config
	registry *core.Registry
	server   *http.Server
	collect  cycleRecord
	history  history.Store
	rings    ringJournal
	started  []core.Waiter
	down     map[string]bool
}

type cycleRecord struct {
	mu        sync.Mutex
	completed time.Time
	ok        int
	failed    int
}

func (c *cycleRecord) record(ok, failed int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed, c.ok, c.failed = time.Now(), ok, failed
}

func (c *cycleRecord) read() (completed time.Time, ok, failed int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.completed, c.ok, c.failed
}

func New(cfg *config.Config, buckets []core.Bucket, getenv func(string) string) *App {
	registry := core.NewRegistry()
	for _, b := range buckets {
		registry.Register(b)
	}
	return newApp(cfg, registry, goapi.SharedTokenSource(getenv))
}

func newApp(cfg *config.Config, registry *core.Registry, tokens goapi.TokenSource) *App {
	verifier := authn.New(cfg.JWKSURL(), cfg.IssuerURL(), cfg.SupabaseJWTSecret, nil)

	staticFS, err := webui.FS()
	if err != nil {
		slog.Error("overseer: embedded SPA unavailable", "error", err)
	}

	store := history.Open(cfg.HistoryPath, history.WithSchemaTimeout(cfg.HistorySchemaTimeout))
	wireSeries(registry, store)

	a := &App{cfg: cfg, registry: registry, history: store, rings: ringJournal{log: store}, down: map[string]bool{}}
	handler := shell.NewHandler(registry,
		shell.WithVerifier(verifier),
		shell.WithOwnerUserID(cfg.OwnerUserID),
		shell.WithStaticFS(staticFS),
		shell.WithClientConfig(shell.ClientConfig{
			SupabaseURL:     cfg.SupabaseURL,
			SupabaseAnonKey: cfg.SupabaseAnonKey,
		}),
		shell.WithCollectStatus(a.collectStatus),
		shell.WithCredentialHealth(credentialHealth(tokens)),
		shell.WithSeries(store),
	)
	a.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return a
}

func (a *App) collectStatus() shell.CollectStatus {
	completed, ok, failed := a.collect.read()
	return shell.CollectStatus{
		Healthy:   !completed.IsZero() && time.Since(completed) <= a.stalenessBudget(ok+failed),
		LastCycle: completed,
		OK:        ok,
		Failed:    failed,
	}
}

func credentialHealth(tokens goapi.TokenSource) func() goapi.CredentialHealth {
	refreshing, isRefreshing := tokens.(*goapi.RefreshingTokenSource)
	if !isRefreshing {
		return nil
	}
	return refreshing.Health
}

func (a *App) stalenessBudget(buckets int) time.Duration {
	return time.Duration(buckets)*a.cfg.BucketTimeout + missedTicks*a.cfg.TickInterval
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	pruned := a.startPruner(ctx)
	defer a.releaseHistory(cancel, pruned)

	a.rings.restore(ctx, a.registry)
	a.collectAll(ctx)
	a.startBuckets(ctx)
	ticked := a.startTickLoop(ctx)
	defer func() {
		cancel()
		a.awaitTickLoop(ticked)
	}()

	served := make(chan error, 1)
	go func() {
		slog.Info("overseer listening", "addr", a.server.Addr, "config", a.cfg)
		served <- a.server.ListenAndServe()
	}()

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
		slog.Info("overseer shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), a.shutdownBudget())
	defer shutdownCancel()
	err := a.server.Shutdown(shutdownCtx)
	<-served
	return err
}

func (a *App) shutdownBudget() time.Duration {
	if a.cfg.ShutdownBudget > 0 {
		return a.cfg.ShutdownBudget
	}
	return defaultShutdownBudget
}

func (a *App) awaitTickLoop(ticks *panicguard.Group) {
	budget := a.shutdownBudget()
	stopped := panicguard.WaitWithin(budget, func() {
		if err := ticks.Wait(); err != nil {
			slog.Error("overseer.collect.tick_loop_panicked", "error", err)
		}
	})
	if !stopped {
		slog.Warn("overseer.shutdown.tick_loop_did_not_stop", "budget", budget)
	}
}

func (a *App) startTickLoop(ctx context.Context) *panicguard.Group {
	var ticks panicguard.Group
	ticks.Go(func() { a.tickLoop(ctx) })
	return &ticks
}

func wireSeries(registry *core.Registry, series core.Series) {
	for _, b := range registry.Buckets() {
		if w, ok := b.(core.SeriesWriter); ok {
			safeUseSeries(b.Meta().ID, w, series)
		}
	}
}

func safeUseSeries(id string, w core.SeriesWriter, series core.Series) {
	panicguard.Contain(context.Background(), "overseer.start.bucket_panic", func() { w.UseSeries(series) }, "bucket", id, "stage", "UseSeries")
}

func (a *App) startPruner(ctx context.Context) <-chan struct{} {
	pruned := make(chan struct{})
	go func() {
		defer close(pruned)
		a.history.RunPruner(ctx)
	}()
	return pruned
}

func (a *App) releaseHistory(cancel context.CancelFunc, pruned <-chan struct{}) {
	cancel()
	<-pruned
	for _, w := range a.started {
		w.Wait()
	}
	a.rings.flushAndDetach()
	if err := a.history.Close(); err != nil {
		slog.Warn("history.close_failed", "error", err)
	}
}

func (a *App) startBuckets(ctx context.Context) {
	for _, b := range a.registry.Buckets() {
		if s, ok := b.(core.Starter); ok {
			safeStart(ctx, b.Meta().ID, s)
		}
		if w, ok := b.(core.Waiter); ok {
			a.started = append(a.started, w)
		}
	}
}

func safeStart(ctx context.Context, id string, s core.Starter) {
	panicguard.Contain(ctx, "overseer.start.bucket_panic", func() { s.Start(ctx) }, "bucket", id)
}

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

func (a *App) collectAll(ctx context.Context) {
	var ok, failed int
	for _, b := range a.registry.Buckets() {
		id := b.Meta().ID
		if err := a.collectOne(ctx, b); err != nil {
			a.noteDown(ctx, id, err)
			failed++
			continue
		}
		a.noteUp(ctx, id)
		ok++
	}
	a.rings.persist()
	a.collect.record(ok, failed)
	slog.DebugContext(ctx, "overseer.collect.cycle", "ok", ok, "failed", failed)
}

func (a *App) noteDown(ctx context.Context, id string, err error) {
	if a.down[id] {
		return
	}
	a.down[id] = true

	var panicErr *bucketPanicError
	if errors.As(err, &panicErr) {
		slog.ErrorContext(ctx, "overseer.collect.bucket_panic", "bucket", id, "stage", panicErr.stage, "recover", panicErr.value)
		return
	}
	slog.WarnContext(ctx, "overseer.collect.source_down", "bucket", id, "error", err)
}

func (a *App) noteUp(ctx context.Context, id string) {
	if !a.down[id] {
		return
	}
	delete(a.down, id)
	slog.InfoContext(ctx, "overseer.collect.source_up", "bucket", id)
}

type bucketPanicError struct {
	stage string
	value any
}

func (e *bucketPanicError) Error() string {
	return fmt.Sprintf("bucket panicked in %s: %v", e.stage, e.value)
}

func (a *App) collectOne(ctx context.Context, b core.Bucket) error {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.BucketTimeout)
	defer cancel()

	signals, err := safeCollect(ctx, b)
	if err != nil {
		return err
	}
	return safeStore(b, signals)
}

func safeCollect(ctx context.Context, b core.Bucket) ([]core.Signal, error) {
	var signals []core.Signal
	var collectErr error
	if perr := panicguard.Try(func() { signals, collectErr = b.Collect(ctx) }); perr != nil {
		return nil, bucketPanic("Collect", perr)
	}
	return signals, collectErr
}

func safeStore(b core.Bucket, signals []core.Signal) error {
	if perr := panicguard.Try(func() { b.Store(signals) }); perr != nil {
		return bucketPanic("Store", perr)
	}
	return nil
}

func bucketPanic(stage string, perr error) *bucketPanicError {
	value, _ := panicguard.Recovered(perr)
	return &bucketPanicError{stage: stage, value: value}
}
