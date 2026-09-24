// Package app is Overseer's composition root: it loads config, sets up logging,
// drives the registered buckets' collect cycle and serves the shell. It knows
// the registry and the shell, never a concrete bucket.
package app

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
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

const shutdownBudget = 15 * time.Second

// missedTicks is how many ticks the loop may miss, on top of the slowest cycle its
// configuration permits, before /health calls it wedged.
const missedTicks = 3

// App holds the wired runtime.
type App struct {
	cfg      *config.Config
	registry *core.Registry
	server   *http.Server
	collect  cycleRecord
	history  history.Store
	// down marks, per bucket ID, whether that source failed last cycle, so an
	// outage logs one down->up transition instead of one WARN per bucket per tick.
	// It is owned by the collect path: Run's synchronous pass and the single
	// tickLoop goroutine never overlap, so unlike collect it needs no lock.
	down map[string]bool
}

// cycleRecord is the last completed collect cycle. The tickLoop goroutine writes it
// and /health's request goroutine reads it, so every access holds the lock.
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

// New wires the app from config against the process-wide bucket registry, which
// buckets have already self-registered into via their package init. It builds the
// Supabase JWT verifier (JWKS-backed, bound to the project's issuer and audience,
// optional HS256 secret), embeds the built SPA
// and serves the JSON API + SSE stream behind the owner-only guard. A missing
// embedded SPA is logged, not fatal: the API still serves so the outlives-the-app
// backstop holds even if the build step was skipped.
func New(cfg *config.Config) *App {
	verifier := authn.New(cfg.JWKSURL(), cfg.IssuerURL(), cfg.SupabaseJWTSecret, nil)

	staticFS, err := webui.FS()
	if err != nil {
		slog.Error("overseer: embedded SPA unavailable", "error", err)
	}

	store := history.Open(cfg.HistoryPath)
	wireSeries(core.Default, store)

	a := &App{cfg: cfg, registry: core.Default, history: store, down: map[string]bool{}}
	handler := shell.NewHandler(core.Default,
		shell.WithVerifier(verifier),
		shell.WithOwnerUserID(cfg.OwnerUserID),
		shell.WithStaticFS(staticFS),
		shell.WithClientConfig(shell.ClientConfig{
			SupabaseURL:     cfg.SupabaseURL,
			SupabaseAnonKey: cfg.SupabaseAnonKey,
		}),
		shell.WithCollectStatus(a.collectStatus),
		shell.WithSeries(store),
	)
	a.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return a
}

// collectStatus answers /health with the collect loop's own liveness. A loop that
// has completed no cycle is unhealthy: Run collects once synchronously before the
// listener opens, so a serving Overseer with no cycle behind it never started one.
func (a *App) collectStatus() shell.CollectStatus {
	completed, ok, failed := a.collect.read()
	return shell.CollectStatus{
		Healthy:   !completed.IsZero() && time.Since(completed) <= a.stalenessBudget(ok+failed),
		LastCycle: completed,
		OK:        ok,
		Failed:    failed,
	}
}

// stalenessBudget is how long the loop may go without completing a cycle before it
// counts as wedged: the slowest cycle its configuration permits — each of the given
// buckets burning its full deadline — plus a few missed ticks. A watched app that is
// merely down cannot turn the liveness backstop red while the loop still runs.
func (a *App) stalenessBudget(buckets int) time.Duration {
	return time.Duration(buckets)*a.cfg.BucketTimeout + missedTicks*a.cfg.TickInterval
}

// Run serves until the process is signalled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	pruned := a.startPruner(ctx)
	defer a.releaseHistory(cancel, pruned)

	a.collectAll(ctx) // one synchronous pass so the first render has data
	a.startBuckets(ctx)
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

func wireSeries(registry *core.Registry, series core.Series) {
	for _, b := range registry.Buckets() {
		if w, ok := b.(core.SeriesWriter); ok {
			safeUseSeries(b.Meta().ID, w, series)
		}
	}
}

func safeUseSeries(id string, w core.SeriesWriter, series core.Series) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overseer.start.bucket_panic", "bucket", id, "stage", "UseSeries", "recover", rec)
		}
	}()
	w.UseSeries(series)
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
	if err := a.history.Close(); err != nil {
		slog.Warn("history.close_failed", "error", err)
	}
}

// startBuckets invokes each bucket's optional Start hook once, before the tick loop,
// with the app-lifetime ctx — cancelled only at shutdown, never wrapped in the
// per-bucket collect deadline collectOne imposes. A bucket that owns background work
// (the security self-test scheduler, a source pump) launches it here so the per-tick
// timeout cannot cancel it after a single run and freeze it (#1812/#1950); the loop
// it spawns exits when this ctx is cancelled at shutdown. A bucket with no background
// work implements no Starter and is skipped.
func (a *App) startBuckets(ctx context.Context) {
	for _, b := range a.registry.Buckets() {
		if s, ok := b.(core.Starter); ok {
			safeStart(ctx, b.Meta().ID, s)
		}
	}
}

// safeStart drives one bucket's Start hook, containing a panic the way safeCollect
// and safeStore do on the tick path. Start runs synchronously at boot, so an
// unguarded panic here would abort startup and take every other bucket's panel down
// with it — the additive-buckets invariant must hold on the lifecycle hook too. A
// crashing Start is a code bug, so it surfaces at ERROR; the bucket simply runs
// without its background loop rather than killing the shell.
func safeStart(ctx context.Context, id string, s core.Starter) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "overseer.start.bucket_panic", "bucket", id, "recover", rec)
		}
	}()
	s.Start(ctx)
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
// down is skipped and its down->up transition logged once, not once per tick; it
// keeps serving its last-known state and the shell stays up (the outlives-the-app
// invariant in the small). A bucket that panics in Collect or Store is contained
// too: collectAll runs inside the tickLoop goroutine, where an unrecovered panic
// would crash the whole process, so the degrade-don't-crash invariant must hold
// here just as safeRender enforces it on the render side.
//
// Each cycle records its ok/failed counts and completion time in-process, where
// /health reads them (#1812): that surface, not a per-tick log line, is the loop's
// liveness signal. The cycle still emits an "overseer.collect.cycle" line at DEBUG
// for anyone tailing logs, but steady-state success is otherwise silent — during an
// outage the logs carry transitions, not a heartbeat drowning them.
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
	a.collect.record(ok, failed)
	slog.DebugContext(ctx, "overseer.collect.cycle", "ok", ok, "failed", failed)
}

// noteDown logs a bucket's failure once — on the tick its source goes down, not
// every tick it stays down — so a sustained outage is one line per bucket, not the
// per-tick WARN flood it used to be during exactly the incident when logs matter. A
// panic surfaces at ERROR (a bucket crashing is a code bug); a merely-unreachable
// source at WARN. The recover sites hand the failure here rather than logging it, so
// this is the single place that both picks the level and rate-limits the line.
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

// noteUp logs a bucket's recovery once, on the tick its source comes back after a
// recorded down transition, closing the outage in the log the same way noteDown
// opened it. A bucket that was never down stays silent.
func (a *App) noteUp(ctx context.Context, id string) {
	if !a.down[id] {
		return
	}
	delete(a.down, id)
	slog.InfoContext(ctx, "overseer.collect.source_up", "bucket", id)
}

// bucketPanicError is the failure safeCollect and safeStore return when a bucket
// panics, kept distinct from a source error so collectAll can log a crash at ERROR
// and a merely-down source at WARN. The recover sites capture the panic instead of
// logging it, so the transition tracker in collectAll is the one place that decides
// the level and rate-limits a sustained failure to a single line.
type bucketPanicError struct {
	stage string
	value any
}

func (e *bucketPanicError) Error() string {
	return fmt.Sprintf("bucket panicked in %s: %v", e.stage, e.value)
}

// collectOne drives one bucket's Collect -> Store under its own deadline and returns
// the failure, if any, for collectAll to log and count. The deadline is the
// containment the Bucket contract does not promise: buckets run serially in the
// single tickLoop goroutine, so a Collect that waits forever on a slow source would
// hold every other bucket behind it and end the cycle. Cancellation is cooperative —
// it reaches a bucket blocked on a context-aware call (every goapi-backed one), not
// a bucket that blocks without watching ctx.
func (a *App) collectOne(ctx context.Context, b core.Bucket) error {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.BucketTimeout)
	defer cancel()

	signals, err := safeCollect(ctx, b)
	if err != nil {
		return err
	}
	return safeStore(b, signals)
}

// safeCollect drives one bucket's Collect, converting a panic into a bucketPanicError
// so a single misbehaving bucket cannot crash the background collect loop. It mirrors
// shell.safeRender: the plugin contract promises containment for every bucket,
// present and future, not only on render.
func safeCollect(ctx context.Context, b core.Bucket) (signals []core.Signal, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			signals, err = nil, &bucketPanicError{stage: "Collect", value: rec}
		}
	}()
	return b.Collect(ctx)
}

// safeStore drives one bucket's Store, containing a panic for the same reason
// safeCollect does: it runs in the tickLoop goroutine. A panicking Store returns a
// bucketPanicError so collectAll counts it as failed and logs the crash once, never
// flattering a store that stored nothing as a success.
func safeStore(b core.Bucket, signals []core.Signal) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = &bucketPanicError{stage: "Store", value: rec}
		}
	}()
	b.Store(signals)
	return nil
}
