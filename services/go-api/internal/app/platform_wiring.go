package app

import (
	"altune/go-api/internal/app/bridges/monitoringfromauth"
	"altune/go-api/internal/app/bridges/monitoringfromdownload"
	"altune/go-api/internal/app/bridges/monitoringfromjobs"
	"altune/go-api/internal/app/bridges/monitoringfromleader"
	"altune/go-api/internal/app/bridges/monitoringfromlibrary"
	"altune/go-api/internal/app/bridges/monitoringfromplayback"
	"altune/go-api/internal/app/bridges/monitoringfromrequestmetrics"
	"altune/go-api/internal/app/bridges/monitoringfromsearch"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config, metrics *telemetry.Registry) (*auth.SupabaseJWTVerifier, error) {
	r0, err := auth.New(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		clock.Now,
		random.Real,
		auth.NewAuthMetrics(metrics),
		auth.NoopTokenRevoker(),
		auth.DefaultJWKSSchedule(),
	)
	if err != nil {
		return r0, fmt.Errorf("call failed: %w", err)
	}
	return r0, nil
}

func authMiddleware(verifier auth.TokenVerifier, metrics *telemetry.Registry) func(http.Handler) http.Handler {
	return auth.MustMiddleware(verifier, clock.Now, auth.NewAuthMetrics(metrics), auth.DefaultFailureLimits)
}

func (a *application) connectAuth(ctx context.Context) (auth.TokenVerifier, error) {
	supaVerifier, err := newAuthVerifier(ctx, a.cfg, a.metrics)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	a.probes.Auth = supaVerifier.CheckHealth
	return supaVerifier, nil
}

func (a *application) dependencyHealth(ctx context.Context) monitoring.DependencyHealth {
	return monitoring.CheckDependencies(ctx, clock.Now, a.probes, monitoring.DefaultProbeTimeout)
}

const alertCheckInterval = 30 * time.Second

func (a *application) startAlertMonitor(ctx context.Context) error {
	notifier := monitoring.NewAlertNotifier(ctx, alertNotifierSettings(a.cfg))
	conditions, err := a.alertConditions(monitoringfromsearch.NewCoverageEvents(search.NewPostgresEventStore(a.pool, clock.Now)))
	if err != nil {
		return err
	}
	monitor, err := newAlertMonitor(notifier, alertCheckInterval, conditions)
	if err != nil {
		return err
	}
	a.alertMonitor = monitor
	a.jobs.Ticker(jobAlertMonitor, alertCheckInterval, monitor.Pass)
	return nil
}

func newAlertMonitor(notifier monitoring.AlertNotifier, interval time.Duration, conditions []monitoring.Condition) (*monitoring.AlertMonitor, error) {
	monitor, err := monitoring.NewAlertMonitor(notifier, interval, clock.Now, conditions...)
	if err != nil {
		return nil, fmt.Errorf("alert monitor: %w", err)
	}
	return monitor, nil
}

func (a *application) alertConditions(coverage monitoring.CoverageEvents) ([]monitoring.Condition, error) {
	r0, err := monitoring.AlertConditions(monitoring.AlertSources{
		DependencyHealth: a.dependencyHealth,
		Coverage:         coverage,
		Sessions:         monitoringfromdownload.NewSourceSessions(a.download),
		Jobs:             a.alertableJobHealth(),
		Requests:         monitoringfromrequestmetrics.NewRequestLatency(a.latency),
		Clock:            clock.Now,
	})
	if err != nil {
		return r0, fmt.Errorf("conditions: %w", err)
	}
	return r0, nil
}

func (a *application) alertableJobHealth() monitoring.JobHealthReader {
	var names []string
	for _, name := range alertableJobs() {
		names = append(names, string(name))
	}
	return monitoringfromjobs.NewJobHealth(names, func(name string) monitoringfromjobs.Control {
		return a.jobs.Control(jobName(name))
	})
}

func (a *application) wireObserve(ctx context.Context, r *chi.Mux, verifier auth.TokenVerifier, tap *monitoring.EventTap) error {
	if err := a.startObserveSources(ctx, tap); err != nil {
		return err
	}
	deps := monitoring.Deps{
		Health:      a.dependencyHealth,
		Logs:        a.logRing,
		Events:      a.eventFeed,
		Eval:        a.evalMeter,
		Acquisition: a.observeAcquisition(),
		LiveMetrics: monitoring.NewLiveMetrics(a.liveSources()),
		Discography: monitoringfromsearch.NewDiscographyQualityReader(search.NewPostgresEventStore(a.pool, clock.Now)),
		Shutdown:    a.lifecycleDone,
	}
	mountObserve(r, verifier, a.metrics, a.cfg.OverseerPrincipalID, monitoring.New(clock.Now, clock.NewTicker, deps))
	return nil
}

func (a *application) startObserveSources(ctx context.Context, tap *monitoring.EventTap) error {
	a.eventFeed = monitoring.NewEventFeed()
	a.eventFeed.Start(ctx, tap)
	meter, err := newEvalMeter(&a.jobs, a.evalMeterRunner())
	if err != nil {
		return err
	}
	a.evalMeter = meter
	a.jobs.Ticker(jobEvalMeter, monitoring.EvalMeterInterval, meter.Pass)
	return nil
}

func newEvalMeter(runner *jobRunner, run monitoring.EvalMeterRunner) (*monitoring.EvalMeter, error) {
	meter, err := monitoring.NewEvalMeter(clock.Now, run, runner.Control(jobEvalMeter).Disabled)
	if err != nil {
		return nil, fmt.Errorf("eval meter: %w", err)
	}
	return meter, nil
}

func mountObserve(r chi.Router, verifier auth.TokenVerifier, metrics *telemetry.Registry, principalID string, h *monitoring.Handler) {
	r.Route("/observe", func(or chi.Router) {
		or.Use(v1ResponseHeaders)
		or.Use(authMiddleware(verifier, metrics))
		or.Use(monitoring.Gate(principalID))
		h.Register(or)
	})
}

func (a *application) observeAcquisition() monitoring.AcquisitionReader {
	if !a.download.On() {
		return nil
	}
	return monitoringfromdownload.NewAcquisitionReader(a.download.Scheduler())
}

func (a *application) liveSources() monitoring.LiveSources {
	return monitoring.LiveSources{
		Auth:         monitoringfromauth.NewSnapshot(a.metrics),
		Catalog:      monitoringfromlibrary.NewSnapshot(a.metrics),
		Playback:     monitoringfromplayback.NewSnapshot(a.metrics),
		Providers:    monitoringfromsearch.NewProviderSnapshot(a.metrics),
		Latency:      monitoringfromrequestmetrics.NewRequestLatency(a.latency),
		Leader:       monitoringfromleader.NewLeaderCounters(a.jobs.LeaderCounters),
		PoolStats:    func() database.PoolStats { return database.ReadPoolStats(a.pool) },
		Redis:        a.redisClient,
		EventBus:     a.eventBus,
		EventTap:     a.eventTap,
		EventFeed:    a.eventFeed,
		AlertMonitor: a.alertMonitor,
		Background:   a.background,
	}
}

func (a *application) evalMeterRunner() monitoring.EvalMeterRunner {
	return monitoringfromsearch.NewEvalMeterRunner(a.buildEvalRunner())
}
